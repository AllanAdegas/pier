package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"pier/internal/audit"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
)

type auditActorKey struct{}
type auditSummaryKey struct{}
type auditOriginKey struct{}

func auditSummary(r *http.Request, body any) {
	if target, ok := r.Context().Value(auditSummaryKey{}).(*string); ok {
		encoded, err := json.Marshal(body)
		if err == nil {
			*target = audit.Summary(encoded, false)
		}
	}
}

func auditActor(r *http.Request, p identity.Principal) {
	if actor, ok := r.Context().Value(auditActorKey{}).(*identity.Principal); ok {
		*actor = p
	}
}

type captureBody struct {
	io.ReadCloser
	captured bytes.Buffer
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	remaining := 8193 - b.captured.Len()
	if remaining > 0 {
		b.captured.Write(p[:min(n, remaining)])
	}
	return n, err
}

func sensitiveRead(path string) bool {
	if path == "/api/v1/instance/backups/status" {
		return false
	}
	for _, part := range []string{"/env", "/variables", "/kubeconfig", "/backup", "/logs", "/connection", "/inspect", "/compose", "/webhook", "/notifications", "/object"} {
		if strings.Contains(path, part) {
			return true
		}
	}
	return false
}
func (a *API) auditRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if a.Audit == nil || ((r.Method == http.MethodGet || r.Method == http.MethodHead) && !sensitiveRead(r.URL.Path)) {
			next.ServeHTTP(w, r)
			return
		}
		actor := &identity.Principal{}
		if r.URL.Path == "/mcp" {
			if p, err := a.resolvePrincipal(r); err == nil {
				*actor = p
			}
		}
		summary := ""
		origin, ok := r.Context().Value(auditOriginKey{}).(string)
		if !ok {
			origin = a.proxies.clientAddress(r)
		}
		r = r.WithContext(context.WithValue(r.Context(), auditOriginKey{}, origin))
		r = r.WithContext(context.WithValue(context.WithValue(r.Context(), auditActorKey{}, actor), auditSummaryKey{}, &summary))
		recorder := &statusRecorder{ResponseWriter: w, status: 200}
		body := &captureBody{ReadCloser: r.Body}
		if r.Body != nil {
			r.Body = body
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				recorder.status = 500
				a.recordAudit(r, *actor, recorder, body)
				panic(recovered)
			}
			a.recordAudit(r, *actor, recorder, body)
		}()
		next.ServeHTTP(recorder, r)
	})
}
func (a *API) recordAudit(r *http.Request, p identity.Principal, w *statusRecorder, body *captureBody) {
	summary := ""
	if body.captured.Len() > 0 {
		summary = audit.Summary(body.captured.Bytes(), strings.Contains(r.URL.Path, "/env"))
	}
	if custom, ok := r.Context().Value(auditSummaryKey{}).(*string); ok && *custom != "" {
		summary = *custom
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 5*time.Second)
	defer cancel()
	origin, ok := r.Context().Value(auditOriginKey{}).(string)
	if !ok {
		origin = a.proxies.clientAddress(r)
	}
	err := a.Audit.Record(ctx, audit.Entry{OccurredAt: sqlite.FormatTime(time.Now()), UserID: p.User.ID, UserName: p.User.Name, TokenID: p.TokenID, TokenName: p.TokenName, IP: origin, Method: r.Method, Route: r.Pattern, ResourceID: r.PathValue("id"), Status: w.status, Summary: summary})
	if err != nil {
		a.Logger.Error("audit record failed", "error", err)
	}
}
func (a *API) registerAuditRoutes(mux *http.ServeMux) {
	a.route(mux, "GET /api/v1/audit", sessionOnly, a.listAudit)
}
func (a *API) listAudit(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	f := audit.Filter{UserID: q.Get("user_id"), TokenID: q.Get("token_id"), Method: q.Get("method"), Route: q.Get("route"), ResourceID: q.Get("resource_id")}
	for _, item := range []struct {
		name  string
		value *string
	}{{"since", &f.Since}, {"until", &f.Until}} {
		if raw := q.Get(item.name); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return apperr.Invalid("Informe datas no formato RFC3339.")
			}
			*item.value = sqlite.FormatTime(t)
		}
	}
	if raw := q.Get("before"); raw != "" {
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 1 {
			return apperr.Invalid("Cursor de auditoria inválido.")
		}
		f.Before = v
	}
	if raw := q.Get("limit"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 || v > 200 {
			return apperr.Invalid("Use um limite entre 1 e 200 registros.")
		}
		f.Limit = v
	}
	entries, err := a.Audit.List(r.Context(), f)
	if err != nil {
		return err
	}
	return ok(w, entries)
}
