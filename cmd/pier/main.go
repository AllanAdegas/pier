// Command pier runs the Pier deployment panel. `pier mcp` instead bridges an
// MCP client that speaks stdio to the MCP endpoint of a running Pier.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pier/internal/access"
	"pier/internal/application"
	"pier/internal/audit"
	"pier/internal/cluster"
	"pier/internal/config"
	"pier/internal/database"
	"pier/internal/engine"
	"pier/internal/envvar"
	"pier/internal/gitprovider"
	"pier/internal/httpapi"
	"pier/internal/identity"
	"pier/internal/instance"
	"pier/internal/logdrain"
	"pier/internal/mcp"
	"pier/internal/monitor"
	"pier/internal/notify"
	"pier/internal/platform/i18n"
	"pier/internal/platform/oplog"
	"pier/internal/platform/pubsub"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/project"
	"pier/internal/provision"
	"pier/internal/registry"
	"pier/internal/schedule"
	"pier/internal/server"
	"pier/internal/settings"
	"pier/internal/stack"
	"pier/internal/swarm"
	"pier/internal/terminal"
	"pier/internal/updates"
	"pier/internal/version"
	"pier/internal/volumebackup"
	"pier/internal/webui"
)

// terminal is the language of the messages this command prints, from the
// locale of the shell that runs it (the panel itself follows the instance and
// each request).
var shellLanguage = i18n.FromEnvironment(os.Getenv)

func main() {
	command := run
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		command = runMCPBridge
	}
	if len(os.Args) > 1 && os.Args[1] == "restore" {
		command = runRestore
	}
	if len(os.Args) > 1 && os.Args[1] == "admin" {
		command = runAdmin
	}
	if err := command(); err != nil {
		fmt.Fprintln(os.Stderr, "pier:", i18n.Localize(err, shellLanguage))
		os.Exit(1)
	}
}

// runMCPBridge reads PIER_URL and PIER_TOKEN, never flags: a token passed as
// an argument would show up in the process list.
func runMCPBridge() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	bridge := mcp.Bridge{URL: os.Getenv("PIER_URL"), Token: os.Getenv("PIER_TOKEN")}
	return bridge.Run(ctx, os.Stdin, os.Stdout)
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if cfg.DevMode {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	unlock, err := instance.Lock(cfg.DataDir)
	if err != nil {
		return err
	}
	defer unlock()
	key, err := secretbox.LoadOrCreateKey(cfg.SecretKey, cfg.SecretKeyPath())
	if err != nil {
		return err
	}
	db, err := sqlite.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	box, err := openSecretBox(ctx, db, key, logger)
	if err != nil {
		return err
	}
	if err := restrictDataDir(cfg); err != nil {
		return err
	}
	instanceSettings := settings.NewStore(db)
	if err := instanceSettings.LoadLanguage(ctx); err != nil {
		return err
	}

	updateChecker, err := updates.New(updates.Options{
		Enabled: updates.EnabledByEnvironment(os.Getenv("PIER_UPDATE_CHECK")), Current: version.Number,
	})
	if err != nil {
		return err
	}

	// Composition root: every dependency is wired here, explicitly.
	notifications := notify.NewService(db, box, logger)
	serverStore := server.NewStore(db)
	keys := server.NewKeyService(serverStore, box)
	connector := server.NewConnector(serverStore, keys)
	defer connector.Close()
	servers := server.NewService(serverStore, keys, connector)
	if err := servers.EnsureLocal(ctx, cfg.LocalRuntimeDir()); err != nil {
		return err
	}
	if err := configurePanel(ctx, servers, os.Getenv("PIER_DOMAIN")); err != nil {
		return err
	}
	backups := &instance.Service{DB: db, DataDir: cfg.DataDir, Key: key, Box: box}
	go backups.Run(ctx, logger)
	alerts := &monitor.State{DB: db}
	env := envvar.NewStore(db, box)
	logHub := pubsub.NewHub[application.LogEntry]()
	appStore := application.NewStore(db, box)
	deployer := application.NewDeployer(appStore, env, connector, keys, notifications, logHub, logger)
	deployer.SetArchiveDir(cfg.DataDir + string(os.PathSeparator) + "archives")
	deployer.SetDomainChecker(servers)
	dispatcher := application.NewDispatcher(appStore, deployer, logger, cfg.DeployTimeout)
	applications := application.NewService(appStore, env, connector, dispatcher)
	images := registry.New(db, box)
	multi := application.NewMultiServer(db, appStore, project.NewService(db), images)
	if err := multi.Recover(ctx); err != nil {
		return err
	}
	deployer.SetMultiServer(multi)
	applications.SetMultiServer(multi)
	previews := application.NewPreviewService(applications, logger)
	gitProviders := gitprovider.New(db, box)
	gitConnections := gitprovider.NewConnections(gitProviders)
	go gitConnections.RunSweeper(ctx, gitprovider.DefaultSweepInterval, logger)
	gitCredentials := gitprovider.Credentials{App: gitProviders, Connections: gitConnections}
	deployer.SetGitProvider(gitCredentials)
	publicURL := func(ctx context.Context) string {
		current, err := instanceSettings.Get(ctx)
		if err != nil {
			return ""
		}
		return current.PublicURL
	}
	feedbackSources := &feedbackRepositories{applications: applications}
	gitFeedbackQueue := gitprovider.NewQueue(gitProviders, feedbackSources.repository, gitprovider.DefaultRetryPolicy(), logger)
	gitFeedback := application.NewGitFeedback(gitFeedbackQueue, publicURL, logger)
	previews.SetReporter(gitFeedback)
	deployer.SetDeploymentReporter(gitFeedback)
	applications.SetCommitResolver(gitProviders)
	go previews.RunJanitor(ctx, application.DefaultPreviewJanitorInterval)
	databaseStore := database.NewStore(db, box)
	databases := database.NewService(databaseStore, connector, notifications)
	env.SetReferenceResolver(application.DatabaseReferences{Databases: databases})
	application.ConfigureBuild(cfg.Build)
	stacks := stack.NewService(db, env, connector, oplog.NewRegistry())
	projects := project.NewService(db)
	applications.SetDeployGuard(projects)
	stacks.SetDeployGuard(projects)
	stacks.SetDomainChecker(servers)
	stacks.SetGitSupport(box, keys)
	stacks.SetGitProvider(gitCredentials)
	stacks.SetGitFeedback(gitFeedbackQueue, publicURL, logger)
	feedbackSources.stacks = stacks
	go gitFeedbackQueue.Run(ctx)
	clusters := cluster.NewService(db, box, connector, oplog.NewRegistry(), cfg.LocalHost)
	tasks := schedule.NewTaskService(db, applications, connector, notifications)

	volumeBackups := volumebackup.NewService(db, box, connector, volumebackup.NewResourceDirectory(applications, stacks), notifications)
	volumeBackups.SetAlertObserver(alerts)
	serverOperations := server.NewOperations(db, connector)
	scheduler := schedule.NewScheduler(logger, tasks.Jobs, backupJobs(databases), volumeBackupJobs(volumeBackups), serverOperationsJobs(serverOperations))
	reload := func() { scheduler.Reload(context.WithoutCancel(ctx)) }
	tasks.OnChange(reload)
	databases.OnScheduleChange(reload)
	volumeBackups.OnScheduleChange(reload)
	serverOperations.OnChange(reload)
	dockerInstaller := server.NewDockerInstaller(connector, servers.Validate)
	provisioning := provision.NewService(db, box, keys, servers, dockerInstaller)
	if err := provisioning.Recover(ctx); err != nil {
		return err
	}
	logDrains := logdrain.NewService(db, box, connector, logDrainRedactors(db, applications, env), logger)
	go logDrains.Run(ctx)

	if err := databases.RecoverBackups(ctx); err != nil {
		return err
	}
	if err := volumeBackups.Recover(ctx); err != nil {
		return err
	}
	if err := clusters.RecoverProvisioning(ctx); err != nil {
		return err
	}
	if err := dispatcher.Start(ctx, cfg.DeployConcurrency); err != nil {
		return err
	}
	// After the workers start: an interrupted multi-server rollout is resumed
	// or rolled back once; a refusal leaves it for the operator.
	if err := applications.RecoverRollouts(ctx); err != nil {
		logger.Warn("recover multi-server rollouts", "error", err)
	}
	scheduler.Start(ctx)
	defer scheduler.Stop()

	statusMonitor := &monitor.Monitor{
		Servers: servers, Connector: connector, Applications: appStore, Databases: databaseStore, Stacks: stacks,
		Clusters: clusters,
		Alerts:   alerts, Tasks: tasks, BackupService: databases,
		Events: notifications, Interval: cfg.StatusPollInterval, Logger: logger,
	}
	go statusMonitor.Run(ctx)
	metricsHistory := &monitor.HistoryStore{DB: db}
	sampler := &monitor.Sampler{Servers: servers, Source: servers, Store: metricsHistory, Logger: logger}
	go sampler.Run(ctx)

	accessService := access.New(db)
	if err := accessService.Bootstrap(ctx); err != nil {
		return err
	}
	identities := identity.NewService(identity.NewStore(db), cfg.SessionTTL)
	identities.SetSecretBox(box)
	identities.SetResetMailer(notifications, logger)
	if err := announceSetupCode(ctx, identities, logger); err != nil {
		return err
	}
	terminals := terminal.NewService(connector)
	defer terminals.Close()
	apiRateLimit, err := httpapi.ParseAPIRateLimit(os.Getenv("PIER_API_RATE_LIMIT"))
	if err != nil {
		return err
	}
	api := httpapi.New(httpapi.Dependencies{
		APIRateLimit: apiRateLimit,
		Access:       accessService,
		OIDC:         identity.NewOIDC(db, box, strings.Fields(os.Getenv("PIER_OIDC_ISSUERS")), os.Getenv("PIER_OIDC_CALLBACK")),
		GitProviders: gitProviders, GitConnections: gitConnections,
		GitFeedback: gitFeedbackQueue,
		MultiServer: multi,
		Registries:  images,
		Identity:    identities, Instance: backups, Audit: &audit.Store{DB: db}, Monitoring: alerts, MetricsHistory: metricsHistory, CookieSecure: os.Getenv("PIER_COOKIE_SECURE") == "1",
		Servers: servers, ServerOperations: serverOperations, DockerInstaller: dockerInstaller, Provisioning: provisioning, LogDrains: logDrains, Keys: keys, Proxy: server.NewProxyService(connector), Connector: connector,
		Projects: projects, Applications: applications, Previews: previews, LogHub: logHub, Env: env,
		Databases: databases, Stacks: stacks, Swarm: swarm.NewService(db, connector), VolumeBackups: volumeBackups, Engine: engine.NewService(connector), Clusters: clusters,
		Tasks: tasks, Notifications: notifications,
		Terminal: terminals, Settings: instanceSettings, Updates: updateChecker, Logger: logger, WebUI: webui.Files(), SessionTTL: cfg.SessionTTL,
		TrustedProxies: cfg.TrustedProxies,
	})
	return serve(ctx, cfg, api.Handler(), logger, dispatcher)
}

// announceSetupCode logs the code that claims an instance without users:
// only who can read the log of the server (its operator) can create the
// first account.
func announceSetupCode(ctx context.Context, identities *identity.Service, logger *slog.Logger) error {
	code, err := identities.SetupCode(ctx)
	if err != nil || code == "" {
		return err
	}
	logger.Warn(i18n.Instance().Text("instância sem administrador: crie a conta no painel com este código de configuração"),
		"setup_code", code)
	return nil
}

// restrictDataDir keeps the data directory and the database private to the
// user running Pier. MkdirAll and SQLite leave existing or new files with
// looser modes (0755/0644).
func restrictDataDir(cfg config.Config) error {
	if err := os.Chmod(cfg.DataDir, 0o700); err != nil {
		return fmt.Errorf("restrict data dir: %w", err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Chmod(cfg.DatabasePath()+suffix, 0o600)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restrict database file: %w", err)
		}
	}
	return nil
}

// backupJobs adapts database backup policies to scheduler jobs.
func backupJobs(databases *database.Service) schedule.Source {
	return func(ctx context.Context) ([]schedule.Job, error) {
		schedules, err := databases.BackupSchedules(ctx)
		if err != nil {
			return nil, err
		}
		jobs := make([]schedule.Job, len(schedules))
		for i, item := range schedules {
			jobs[i] = schedule.Job{Name: "backup:" + item.DatabaseID, Cron: item.Cron, Run: func() {
				_, _ = databases.StartBackup(context.Background(), item.DatabaseID, database.BackupTriggerScheduled)
			}}
		}
		checks, err := databases.RestoreTestSchedules(ctx)
		if err != nil {
			return nil, err
		}
		for _, item := range checks {
			jobs = append(jobs, schedule.Job{Name: "restore-test:" + item.DatabaseID, Cron: item.Cron, Run: func() { _, _ = databases.StartRestoreTest(context.Background(), item.DatabaseID) }})
		}
		return jobs, nil
	}
}

func serve(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger,
	dispatcher *application.Dispatcher) error {
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
		// Requests inherit the process context so streams end on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
		// No write timeout: log streams (SSE) and backup downloads are long-lived.
	}
	failed := make(chan error, 1)
	go func() {
		logger.Info("pier listening", "addr", cfg.Addr, "data_dir", cfg.DataDir)
		if err := httpServer.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
	}()
	select {
	case err := <-failed:
		return err
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGracePeriod)
	defer cancel()
	_ = httpServer.Shutdown(shutdownCtx)
	// Workers observe the cancelled context, mark their deployment and exit.
	dispatcher.Wait()
	return nil
}
