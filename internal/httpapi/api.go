// Package httpapi exposes the use cases over HTTP: a JSON API under /api/v1,
// unauthenticated webhooks under /webhooks and the embedded web UI.
package httpapi

import (
	"io/fs"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	rbac "pier/internal/access"
	"pier/internal/application"
	"pier/internal/audit"
	"pier/internal/cluster"
	"pier/internal/database"
	"pier/internal/engine"
	"pier/internal/envvar"
	"pier/internal/gitprovider"
	"pier/internal/identity"
	"pier/internal/instance"
	"pier/internal/logdrain"
	"pier/internal/mcp"
	"pier/internal/monitor"
	"pier/internal/notify"
	"pier/internal/platform/i18n"
	"pier/internal/project"
	"pier/internal/provision"
	"pier/internal/registry"
	"pier/internal/schedule"
	"pier/internal/server"
	"pier/internal/settings"
	"pier/internal/stack"
	"pier/internal/terminal"
	"pier/internal/updates"
	"pier/internal/version"
	"pier/internal/volumebackup"
)

// Dependencies are the application services the API delegates to.
type Dependencies struct {
	Access           *rbac.Service
	OIDC             *identity.OIDCService
	ServerOperations *server.Operations
	DockerInstaller  *server.DockerInstaller
	Provisioning     *provision.Service
	LogDrains        *logdrain.Service
	GitProviders     *gitprovider.Service
	GitFeedback      *gitprovider.Queue
	MultiServer      *application.MultiServer
	Registries       *registry.Service
	Instance         *instance.Service
	Audit            *audit.Store
	Monitoring       *monitor.State
	Identity         *identity.Service
	Servers          *server.Service
	Keys             *server.KeyService
	Proxy            *server.ProxyService
	Connector        *server.Connector
	Projects         *project.Service
	Applications     *application.Service
	Previews         *application.PreviewService
	LogHub           *application.LogHub
	Env              *envvar.Store
	Databases        *database.Service
	Stacks           *stack.Service
	VolumeBackups    *volumebackup.Service
	Engine           *engine.Service
	Clusters         *cluster.Service
	Tasks            *schedule.TaskService
	Notifications    *notify.Service
	Settings         *settings.Store
	Updates          *updates.Checker
	Terminal         *terminal.Service
	Logger           *slog.Logger
	WebUI            fs.FS
	SessionTTL       time.Duration

	MetricsHistory *monitor.HistoryStore
	// TrustedProxies may describe, through X-Forwarded-*, the client they
	// forward. See config.TrustedProxies.
	TrustedProxies []netip.Prefix
	CookieSecure   bool
	// APIRateLimit is the requests per minute of each API token; 0 or less
	// disables the limit. See ParseAPIRateLimit.
	APIRateLimit int
}

// API holds the HTTP handlers.
type API struct {
	Dependencies
	proxies proxyTrust
	// addressLimiter throttles guesses per client address; accountLimiter
	// per account, so rotating addresses does not buy more guesses.
	addressLimiter *rateLimiter
	accountLimiter *rateLimiter
	// resetLimiter bounds reset e-mails per address; apiLimiter, API tokens.
	resetLimiter *rateLimiter
	apiLimiter   *tokenBuckets
	// routes records every registered route for the authorization coverage test.
	routes []registeredRoute
}

// New creates the API.
func New(deps Dependencies) *API {
	return &API{
		Dependencies:   deps,
		proxies:        proxyTrust{prefixes: deps.TrustedProxies},
		addressLimiter: newRateLimiter(10, time.Minute),
		accountLimiter: newRateLimiter(20, time.Minute),
		resetLimiter:   newRateLimiter(3, 15*time.Minute),
		apiLimiter:     newTokenBuckets(deps.APIRateLimit),
	}
}

// Handler builds the root handler with every route registered.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// Unauthenticated on purpose: clients use it to check compatibility
	// before they have a credential.
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"version": version.Number, "api": version.API})
	})
	a.registerMultiServerRoutes(mux)
	a.registerInstanceRoutes(mux)
	a.registerAuditRoutes(mux)
	a.registerMonitoringRoutes(mux)
	a.registerMetricsRoutes(mux)
	a.registerAuthRoutes(mux)
	if a.Access != nil {
		a.RegisterAccessRoutes(mux, a.Access)
	}
	if a.OIDC != nil {
		a.RegisterOIDCRoutes(mux, a.OIDC)
	}
	a.registerServerRoutes(mux)
	a.registerServerOperationsRoutes(mux)
	a.registerProvisionRoutes(mux)
	a.registerLogDrainRoutes(mux)
	a.registerProjectRoutes(mux)
	a.registerApplicationRoutes(mux)
	if a.Previews != nil {
		a.registerPreviewRoutes(mux)
	}
	a.registerDatabaseRoutes(mux)
	a.registerStackRoutes(mux)
	a.registerVolumeBackupRoutes(mux)
	a.registerDockerRoutes(mux)
	a.registerClusterRoutes(mux)
	a.registerMiscRoutes(mux)
	a.registerGitProviderRoutes(mux)
	a.registerGitFeedbackRoutes(mux)
	a.registerUpdateRoutes(mux)
	a.registerTerminalRoutes(mux)
	a.registerEngineRoutes(mux)
	a.registerWebhookRoutes(mux)
	mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotFound, errorBody{Error: errorDetail{
			Code: "not_found", Message: i18n.FromContext(r.Context()).Text("Rota inexistente."),
		}})
	}))
	mux.Handle("/", a.webUIHandler())
	// Tools call the API routes above in process, with the agent's token.
	mux.Handle(mcp.Path, mcp.NewHandler(a.auditRequest(mux), a.Identity, a.Logger))
	return withLanguage(a.recoverPanics(a.securityHeaders(a.logRequests(a.auditRequest(requestReadDeadline(mux))))))
}

// access describes what a route demands from the caller.
type access struct {
	ability     identity.Ability
	sessionOnly bool
}

var (
	canRead   = access{ability: identity.AbilityRead}
	canWrite  = access{ability: identity.AbilityWrite}
	canDeploy = access{ability: identity.AbilityDeploy}
	// sessionOnly routes manage credentials and are closed to API tokens.
	sessionOnly = access{ability: identity.AbilityWrite, sessionOnly: true}
)

type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// route registers an authenticated endpoint.
func (a *API) route(mux *http.ServeMux, pattern string, required access, handler handlerFunc) {
	a.routes = append(a.routes, registeredRoute{pattern: pattern, required: required})
	mux.Handle(pattern, a.authenticate(required, a.adapt(handler)))
}

// public registers an endpoint that needs no authentication.
func (a *API) public(mux *http.ServeMux, pattern string, handler handlerFunc) {
	a.routes = append(a.routes, registeredRoute{pattern: pattern, public: true})
	mux.Handle(pattern, a.adapt(handler))
}

func (a *API) adapt(handler handlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			a.writeError(w, r, err)
		}
	})
}
