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
	"syscall"
	"time"

	"pier/internal/application"
	"pier/internal/cluster"
	"pier/internal/config"
	"pier/internal/database"
	"pier/internal/engine"
	"pier/internal/envvar"
	"pier/internal/httpapi"
	"pier/internal/identity"
	"pier/internal/mcp"
	"pier/internal/monitor"
	"pier/internal/notify"
	"pier/internal/platform/oplog"
	"pier/internal/platform/pubsub"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/project"
	"pier/internal/schedule"
	"pier/internal/server"
	"pier/internal/settings"
	"pier/internal/stack"
	"pier/internal/webui"
)

func main() {
	command := run
	if len(os.Args) > 1 && os.Args[1] == "mcp" {
		command = runMCPBridge
	}
	if err := command(); err != nil {
		fmt.Fprintln(os.Stderr, "pier:", err)
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
	key, err := secretbox.LoadOrCreateKey(cfg.SecretKey, cfg.SecretKeyPath())
	if err != nil {
		return err
	}
	box, err := secretbox.New(key)
	if err != nil {
		return err
	}
	db, err := sqlite.Open(ctx, cfg.DatabasePath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := restrictDataDir(cfg); err != nil {
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
	env := envvar.NewStore(db, box)
	logHub := pubsub.NewHub[application.LogEntry]()
	appStore := application.NewStore(db, box)
	deployer := application.NewDeployer(appStore, env, connector, keys, notifications, logHub, logger)
	dispatcher := application.NewDispatcher(appStore, deployer, logger)
	applications := application.NewService(appStore, env, connector, dispatcher)
	databaseStore := database.NewStore(db, box)
	databases := database.NewService(databaseStore, connector, notifications)
	stacks := stack.NewService(db, env, connector, oplog.NewRegistry())
	clusters := cluster.NewService(db, box, connector, oplog.NewRegistry(), cfg.LocalHost)
	tasks := schedule.NewTaskService(db, applications, connector, notifications)

	scheduler := schedule.NewScheduler(logger, tasks.Jobs, backupJobs(databases))
	reload := func() { scheduler.Reload(context.WithoutCancel(ctx)) }
	tasks.OnChange(reload)
	databases.OnScheduleChange(reload)

	if err := databases.RecoverBackups(ctx); err != nil {
		return err
	}
	if err := clusters.RecoverProvisioning(ctx); err != nil {
		return err
	}
	if err := dispatcher.Start(ctx, cfg.DeployConcurrency); err != nil {
		return err
	}
	scheduler.Start(ctx)
	defer scheduler.Stop()

	statusMonitor := &monitor.Monitor{
		Servers: servers, Connector: connector, Applications: appStore, Databases: databaseStore, Stacks: stacks,
		Clusters: clusters,
		Events:   notifications, Interval: cfg.StatusPollInterval, Logger: logger,
	}
	go statusMonitor.Run(ctx)

	identities := identity.NewService(identity.NewStore(db), cfg.SessionTTL)
	if err := announceSetupCode(ctx, identities, logger); err != nil {
		return err
	}
	api := httpapi.New(httpapi.Dependencies{
		Identity: identities,
		Servers:  servers, Keys: keys, Proxy: server.NewProxyService(connector), Connector: connector,
		Projects: project.NewService(db), Applications: applications, LogHub: logHub, Env: env,
		Databases: databases, Stacks: stacks, Engine: engine.NewService(connector), Clusters: clusters,
		Tasks: tasks, Notifications: notifications,
		Settings: settings.NewStore(db), Logger: logger, WebUI: webui.Files(), SessionTTL: cfg.SessionTTL,
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
	logger.Warn("instância sem administrador: crie a conta no painel com este código de configuração",
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
		return jobs, nil
	}
}

func serve(ctx context.Context, cfg config.Config, handler http.Handler, logger *slog.Logger,
	dispatcher *application.Dispatcher) error {
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
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
