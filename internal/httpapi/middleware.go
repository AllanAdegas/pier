package httpapi

import (
	"container/list"
	"context"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

const (
	sessionCookie = "pier_session"
	// clientHeader must accompany cookie-authenticated mutations. Browsers
	// cannot add custom headers to cross-site requests without a CORS
	// preflight (which this API never approves), so it blocks CSRF.
	clientHeader = "X-Pier-Client"
)

type contextKey struct{}

func principalFrom(ctx context.Context) identity.Principal {
	principal, _ := ctx.Value(contextKey{}).(identity.Principal)
	return principal
}

// authenticate resolves the caller from a bearer token or the session cookie
// and enforces what the route requires.
func (a *API) authenticate(required access, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		principal, err := a.resolvePrincipal(r)
		if err != nil {
			a.writeError(w, r, err)
			return
		}
		auditActor(r, principal)
		if principal.ViaToken && !a.admitToken(w, r, principal.TokenID) {
			return
		}
		if required.sessionOnly && principal.ViaToken {
			a.writeError(w, r, apperr.Forbidden("Esta operação exige login no painel."))
			return
		}
		if !principal.Can(required.ability) {
			a.writeError(w, r, apperr.Forbidden("O token não tem a permissão %q.", string(required.ability)))
			return
		}
		if !principal.ViaToken && r.Method != http.MethodGet && r.Header.Get(clientHeader) == "" {
			a.writeError(w, r, apperr.Forbidden("Cabeçalho %s ausente.", clientHeader))
			return
		}
		granted, err := a.authorizeRequest(r, principal, required)
		if err != nil {
			a.writeError(w, r, err)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), contextKey{}, granted.principal))
		if granted.filterKind != "" {
			a.serveFiltered(w, r, principal, granted.filterKind, next)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// admitToken applies the per-token rate limit. Sessions are not limited
// here: their only guessable entry, the login, has its own limiter.
func (a *API) admitToken(w http.ResponseWriter, r *http.Request, tokenID string) bool {
	allowed, wait := a.apiLimiter.take(tokenID)
	if allowed {
		return true
	}
	seconds := retryAfterSeconds(wait)
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	message := i18n.FromContext(r.Context()).Sprintf("Limite de requisições do token excedido. Tente de novo em %d s.", seconds)
	writeJSON(w, http.StatusTooManyRequests, errorBody{Error: errorDetail{Code: "rate_limited", Message: message}})
	return false
}

func (a *API) resolvePrincipal(r *http.Request) (identity.Principal, error) {
	if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found {
		return a.Identity.AuthenticateToken(r.Context(), strings.TrimSpace(bearer))
	}
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || cookie.Value == "" {
		return identity.Principal{}, apperr.Unauthorized("Autenticação necessária.")
	}
	return a.Identity.AuthenticateSession(r.Context(), cookie.Value)
}

func (a *API) setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: a.CookieSecure || a.proxies.isHTTPS(r), MaxAge: int(a.SessionTTL.Seconds()),
	})
}

func (a *API) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: a.CookieSecure || a.proxies.isHTTPS(r), MaxAge: -1,
	})
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		if a.Settings != nil {
			if current, err := a.Settings.Get(r.Context()); err == nil && strings.HasPrefix(current.PublicURL, "https://") {
				header.Set("Strict-Transport-Security", "max-age=31536000")
			}
		}
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; "+
			"img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'")
		next.ServeHTTP(w, r)
	})
}

func (a *API) recoverPanics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				a.Logger.Error("panic in handler", "path", r.URL.Path, "panic", recovered, "stack", string(debug.Stack()))
				writeInternalError(w, r)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status    int
	committed bool
}

func (s *statusRecorder) WriteHeader(status int) {
	if s.committed {
		return
	}
	s.committed = true
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(p []byte) (int, error) {
	if !s.committed {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(p)
}

// Unwrap lets http.ResponseController reach Flush on the real writer (SSE).
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

func (a *API) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/webhooks/") && r.URL.Path != "/mcp" {
			next.ServeHTTP(w, r)
			return
		}
		startedAt := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		level := a.Logger.Debug
		if recorder.status >= 500 {
			level = a.Logger.Warn
		}
		level("http", "method", r.Method, "path", r.URL.Path, "status", recorder.status,
			"duration_ms", time.Since(startedAt).Milliseconds())
	})
}

// rateLimiter bounds memory and does constant work per request. The FIFO order
// is also reset-time order: windows never move on access. Eviction removes the
// oldest window when full; expiration is checked lazily, without a map sweep.
const maxLimiterKeys = 4096

type rateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string]*windowCount
	order  *list.List
	now    func() time.Time
}
type windowCount struct {
	count   int
	resetAt time.Time
	element *list.Element
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: make(map[string]*windowCount), order: list.New(), now: time.Now}
}
func (l *rateLimiter) remove(key string) {
	if entry := l.hits[key]; entry != nil {
		l.order.Remove(entry.element)
		delete(l.hits, key)
	}
}
func (l *rateLimiter) reset(key string) { l.mu.Lock(); defer l.mu.Unlock(); l.remove(key) }
func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry := l.hits[key]
	if entry != nil && !now.Before(entry.resetAt) {
		l.remove(key)
		entry = nil
	}
	if entry == nil {
		if len(l.hits) >= maxLimiterKeys {
			l.remove(l.order.Front().Value.(string))
		}
		entry = &windowCount{resetAt: now.Add(l.window), element: l.order.PushBack(key)}
		l.hits[key] = entry
	}
	if entry.count >= l.limit {
		return false
	}
	entry.count++
	return true
}

// requestReadDeadline limits body reads without a write timeout on streams.
func requestReadDeadline(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			budget := 30 * time.Second
			if strings.HasSuffix(r.URL.Path, "/deploy-archive") {
				budget = 10 * time.Minute
			}
			controller := http.NewResponseController(w)
			_ = controller.SetReadDeadline(time.Now().Add(budget))
			defer controller.SetReadDeadline(time.Time{})
		}
		next.ServeHTTP(w, r)
	})
}
