package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"pier/internal/identity"
)

// filteredWriter holds a collection response until its items are filtered.
type filteredWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (f *filteredWriter) Header() http.Header         { return f.header }
func (f *filteredWriter) WriteHeader(status int)      { f.status = status }
func (f *filteredWriter) Write(p []byte) (int, error) { return f.body.Write(p) }

// serveFiltered runs a collection handler and keeps only the items of
// projects where the caller holds at least the read role. Anything but a
// successful JSON array is passed through when it is an error, and refused
// otherwise: an unexpected shape must not leak unfiltered.
func (a *API) serveFiltered(w http.ResponseWriter, r *http.Request, p identity.Principal, kind string, next http.Handler) {
	buffered := &filteredWriter{header: http.Header{}, status: http.StatusOK}
	next.ServeHTTP(buffered, r)
	for key, values := range buffered.header {
		w.Header()[key] = values
	}
	if buffered.status != http.StatusOK {
		w.WriteHeader(buffered.status)
		_, _ = w.Write(buffered.body.Bytes())
		return
	}
	visible, err := a.visibleItems(r.Context(), p, kind, buffered.body.Bytes())
	if err != nil {
		w.Header().Del("Content-Length")
		a.writeError(w, r, err)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(visible)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(visible)
}

func (a *API) visibleItems(ctx context.Context, p identity.Principal, kind string, body []byte) ([]byte, error) {
	var items []json.RawMessage
	if err := json.Unmarshal(body, &items); err != nil || items == nil {
		return nil, accessDenied()
	}
	kept := make([]json.RawMessage, 0, len(items))
	for _, item := range items {
		var probe struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(item, &probe); err != nil {
			return nil, accessDenied()
		}
		allowed, err := a.readable(ctx, p, kind, probe.ID)
		if err != nil {
			return nil, err
		}
		if allowed {
			kept = append(kept, item)
		}
	}
	encoded, err := json.Marshal(kept)
	return append(encoded, '\n'), err
}

func (a *API) readable(ctx context.Context, p identity.Principal, kind, id string) (bool, error) {
	project, err := a.resolveProject(ctx, kind, id)
	if err != nil || project == "" {
		return false, err
	}
	return a.Access.Check(ctx, p.User.ID, project, string(identity.AbilityRead)) == nil, nil
}
