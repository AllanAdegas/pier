package httpapi

import (
	"context"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
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
		if required.sessionOnly && principal.ViaToken {
			a.writeError(w, r, apperr.Forbidden("Esta operação exige login no painel."))
			return
		}
		if !principal.Can(required.ability) {
			a.writeError(w, r, apperr.Forbidden("O token não tem a permissão \""+string(required.ability)+"\"."))
			return
		}
		if !principal.ViaToken && r.Method != http.MethodGet && r.Header.Get(clientHeader) == "" {
			a.writeError(w, r, apperr.Forbidden("Cabeçalho "+clientHeader+" ausente."))
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), contextKey{}, principal)))
	})
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
		Secure: isHTTPS(r), MaxAge: int(a.SessionTTL.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: isHTTPS(r), MaxAge: -1,
	})
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (a *API) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
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
				writeJSON(w, http.StatusInternalServerError, errorBody{Error: errorDetail{
					Code: "internal_error", Message: "Erro interno. Consulte os logs do Pier.",
				}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
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

// rateLimiter is a fixed-window counter per client address.
type rateLimiter struct {
	limit  int
	window time.Duration
	mu     sync.Mutex
	hits   map[string]*windowCount
}

type windowCount struct {
	count   int
	resetAt time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{limit: limit, window: window, hits: make(map[string]*windowCount)}
}

func (l *rateLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for candidate, entry := range l.hits {
		if now.After(entry.resetAt) {
			delete(l.hits, candidate)
		}
	}
	entry, exists := l.hits[key]
	if !exists {
		l.hits[key] = &windowCount{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	entry.count++
	return entry.count <= l.limit
}

// clientAddress returns the caller's IP. Forwarding headers are only trusted
// when the direct peer is a local proxy.
func clientAddress(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer := net.ParseIP(host)
	if peer != nil && (peer.IsLoopback() || peer.IsPrivate()) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			first, _, _ := strings.Cut(forwarded, ",")
			return strings.TrimSpace(first)
		}
	}
	return host
}
