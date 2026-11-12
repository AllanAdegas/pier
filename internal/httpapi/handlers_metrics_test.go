package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pier/internal/identity"
	"pier/internal/monitor"
)

func metricsRequest(t *testing.T, a *API, path, secret string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	a.registerMetricsRoutes(mux)
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if secret != "" {
		r.Header.Set("Authorization", "Bearer "+secret)
	}
	w := httptest.NewRecorder()
	a.auditRequest(mux).ServeHTTP(w, r)
	return w
}

func TestMetricsHistoryRoutes(t *testing.T) {
	ctx := context.Background()
	a := platformAPI(t)
	a.MetricsHistory = &monitor.HistoryStore{DB: a.Audit.DB}
	cpu := 12.5
	if err := a.MetricsHistory.Record(ctx, time.Now().Add(-5*time.Minute), map[string]monitor.Sample{"server:s1": {CPU: &cpu}}); err != nil {
		t.Fatal(err)
	}
	user, _ := a.Identity.CreateUser(ctx, "Ana", "ana@example.com", "test-password-long")
	_, reader, _ := a.Identity.CreateToken(ctx, user.ID, "read", []identity.Ability{identity.AbilityRead})

	if w := metricsRequest(t, a, "/api/v1/servers/s1/metrics/history?range=1h", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", w.Code)
	}
	w := metricsRequest(t, a, "/api/v1/servers/s1/metrics/history?range=1h", reader)
	if w.Code != http.StatusOK {
		t.Fatalf("read token: %d %s", w.Code, w.Body)
	}
	var raw struct {
		Range  string `json:"range"`
		Step   int    `json:"step_seconds"`
		Series struct {
			T   []int64    `json:"t"`
			CPU []*float64 `json:"cpu"`
		} `json:"series"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Range != "1h" || raw.Step != 60 || len(raw.Series.T) != 1 || *raw.Series.CPU[0] != 12.5 {
		t.Fatalf("shape: %s", w.Body)
	}
}

func TestMetricsHistoryRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	a := platformAPI(t)
	a.MetricsHistory = &monitor.HistoryStore{DB: a.Audit.DB}
	user, _ := a.Identity.CreateUser(ctx, "Ana", "ana@example.com", "test-password-long")
	_, secret, _ := a.Identity.CreateToken(ctx, user.ID, "read", []identity.Ability{identity.AbilityRead})
	cases := map[string]int{
		"/api/v1/servers/s1/metrics/history?range=1y":                 http.StatusUnprocessableEntity,
		"/api/v1/resources/widgets/x/metrics/history?range=1h":        http.StatusNotFound,
		"/api/v1/resources/applications/missing/metrics/history":      http.StatusNotFound,
		"/api/v1/resources/stack/missing/metrics/history?range=7d":    http.StatusNotFound,
		"/api/v1/resources/applications/x/metrics/history?range=../1": http.StatusUnprocessableEntity,
	}
	for path, want := range cases {
		if w := metricsRequest(t, a, path, secret); w.Code != want {
			t.Errorf("%s: %d, want %d (%s)", path, w.Code, want, w.Body)
		}
	}
}
