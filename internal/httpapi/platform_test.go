package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/application"
	"pier/internal/audit"
	"pier/internal/identity"
	"pier/internal/instance"
	"pier/internal/monitor"
	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/project"
	"pier/internal/settings"
	"pier/internal/stack"
)

func platformAPI(t *testing.T) *API {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secretbox.New("platform-test-instance-key")
	return New(Dependencies{Access: testAccess(t,db), Identity: identity.NewService(identity.NewStore(db), time.Hour), Instance: &instance.Service{DB: db, DataDir: dir, Key: "platform-test-instance-key", Box: box}, Audit: &audit.Store{DB: db}, Monitoring: &monitor.State{DB: db}, Settings: settings.NewStore(db), Projects: project.NewService(db), Applications: application.NewService(application.NewStore(db, box), nil, nil, nil), Stacks: stack.NewService(db, nil, nil, nil), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), SessionTTL: time.Hour})
}
func TestLimiterMemoryIsBoundedWithoutSweepingAndWindowsExpire(t *testing.T) {
	l := newRateLimiter(2, time.Minute)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < maxLimiterKeys*4; i++ {
		if !l.allow(fmt.Sprint(i)) {
			t.Fatal("new key denied")
		}
		if len(l.hits) > maxLimiterKeys || l.order.Len() != len(l.hits) {
			t.Fatal("unbounded limiter")
		}
	}
	if l.allow(fmt.Sprint(maxLimiterKeys*4-1)) != true || l.allow(fmt.Sprint(maxLimiterKeys*4-1)) != false {
		t.Fatal("limit not enforced")
	}
	now = now.Add(time.Minute)
	if !l.allow(fmt.Sprint(maxLimiterKeys*4 - 1)) {
		t.Fatal("expired window stayed denied")
	}
	l.reset(fmt.Sprint(maxLimiterKeys*4 - 1))
	if !l.allow(fmt.Sprint(maxLimiterKeys*4 - 1)) {
		t.Fatal("success reset failed")
	}
}
func BenchmarkLimiterAtCapacity(b *testing.B) {
	l := newRateLimiter(10, time.Minute)
	for i := 0; i < maxLimiterKeys; i++ {
		l.allow(fmt.Sprint(i))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l.allow("fixed")
	}
}
func TestAddressDenialDoesNotSpendAccountBudget(t *testing.T) {
	a := New(Dependencies{})
	r := forwarded("203.0.113.1:1", "")
	for i := 0; i < 100; i++ {
		a.allowGuess(r, "login:victim@example.com")
	}
	if n := a.accountLimiter.hits["login:victim@example.com"].count; n != 10 {
		t.Fatalf("spent %d guesses", n)
	}
}
func TestDuplicateForwardingHeadersUseRightmostUntrustedHop(t *testing.T) {
	r := forwarded("10.0.0.2:1", "198.51.100.1")
	r.Header.Add("X-Forwarded-For", "203.0.113.7, 10.0.0.3")
	if got := trustOnly("10.0.0.0/8").clientAddress(r); got != "203.0.113.7" {
		t.Fatalf("address %s", got)
	}
}
func TestLongEmailIsRejectedBeforeBuildingLimiterKeys(t *testing.T) {
	a := platformAPI(t)
	r := httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"`+strings.Repeat("a", 321)+`"}`))
	err := a.login(httptest.NewRecorder(), r)
	if e, ok := apperr.As(err); !ok || e.Kind != apperr.KindInvalid {
		t.Fatalf("err %v", err)
	}
	if len(a.accountLimiter.hits) != 0 {
		t.Fatal("created oversized account key")
	}
}
func TestLogoutClearsCookieAndReportsDatabaseFailure(t *testing.T) {
	a := platformAPI(t)
	a.Instance.DB.Close()
	r := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: "old-session"})
	w := httptest.NewRecorder()
	if err := a.logout(w, r); err == nil {
		t.Fatal("suppressed revocation failure")
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("failed to clear cookie")
	}
}
func TestPasswordChangeIsThrottledBeforeBcrypt(t *testing.T) {
	a := platformAPI(t)
	for i := 0; i < 20; i++ {
		a.accountLimiter.allow("password:user")
	}
	r := httptest.NewRequest("POST", "/api/v1/me/password", strings.NewReader(`{"current":"wrong","next":"new-password"}`))
	r = r.WithContext(context.WithValue(r.Context(), contextKey{}, identity.Principal{User: identity.User{ID: "user"}}))
	w := httptest.NewRecorder()
	if err := a.changePassword(w, r); err != nil || w.Code != 429 {
		t.Fatalf("status %d err %v", w.Code, err)
	}
}
func TestForcedSecureCookiesAndHTTPSPublicURLHSTS(t *testing.T) {
	a := platformAPI(t)
	a.CookieSecure = true
	r := httptest.NewRequest("GET", "http://panel.invalid/", nil)
	w := httptest.NewRecorder()
	a.setSessionCookie(w, r, "session")
	if !w.Result().Cookies()[0].Secure {
		t.Fatal("Secure option ignored")
	}
	if _, err := a.Settings.Update(r.Context(), settings.Settings{InstanceName: "Pier", PublicURL: "https://panel.invalid"}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	a.securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, r)
	if w.Header().Get("Strict-Transport-Security") == "" {
		t.Fatal("HSTS absent")
	}
}

type deadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *deadlineWriter) SetReadDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return nil
}
func TestBodyDeadlinesPreserveStreamsAndAllowArchiveUploads(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		budget       time.Duration
	}{{"POST", "/api/v1/auth/login", 30 * time.Second}, {"POST", "/api/v1/auth/setup", 30 * time.Second}, {"POST", "/webhooks/applications/id/github", 30 * time.Second}, {"POST", "/api/v1/applications/id/deploy-archive", 10 * time.Minute}, {"GET", "/api/v1/deployments/id/logs/stream", 0}} {
		t.Run(tc.path, func(t *testing.T) {
			w := &deadlineWriter{ResponseRecorder: httptest.NewRecorder()}
			before := time.Now()
			requestReadDeadline(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
			if tc.budget == 0 {
				if len(w.deadlines) != 0 {
					t.Fatal("deadline on stream")
				}
				return
			}
			if len(w.deadlines) != 2 || !w.deadlines[1].IsZero() || w.deadlines[0].Sub(before) < tc.budget || w.deadlines[0].Sub(before) > tc.budget+time.Second {
				t.Fatalf("deadlines %v", w.deadlines)
			}
		})
	}
}
func TestAuditRecordsActorMaskedBodyRouteAndSensitiveRead(t *testing.T) {
	a := platformAPI(t)
	user, err := a.Identity.CreateUser(context.Background(), "Alice", "alice@example.com", "test-password-long")
	if err != nil {
		t.Fatal(err)
	}
	token, secret, err := a.Identity.CreateToken(context.Background(), user.ID, "ci", []identity.Ability{identity.AbilityWrite})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.route(mux, "POST /api/v1/test/{id}", canWrite, func(w http.ResponseWriter, r *http.Request) error {
		_, err := decode[map[string]any](w, r)
		if err != nil {
			return err
		}
		return created(w, map[string]bool{"ok": true})
	})
	a.route(mux, "GET /api/v1/test/{id}/logs", canRead, func(w http.ResponseWriter, r *http.Request) error { return ok(w, []string{}) })
	for _, method := range []string{"POST", "GET"} {
		path := "/api/v1/test/resource"
		if method == "GET" {
			path += "/logs"
		}
		r := httptest.NewRequest(method, path, strings.NewReader(`{"password":"dummy-audit-secret","name":"safe"}`))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		a.auditRequest(mux).ServeHTTP(w, r)
		if w.Code >= 400 {
			t.Fatal(w.Body.String())
		}
	}
	entries, err := a.Audit.List(context.Background(), audit.Filter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %v err %v", entries, err)
	}
	e := entries[1]
	if e.UserID != user.ID || e.TokenID != token.ID || e.TokenName != "ci" || e.ResourceID != "resource" || e.Route != "POST /api/v1/test/{id}" || e.Status != 201 || strings.Contains(e.Summary, "dummy-audit-secret") {
		t.Fatalf("audit %+v", e)
	}
}
func TestPublishedUptimeURLCannotEscapeOriginOrPublishedPath(t *testing.T) {
	routes := []string{"https://app.example.com/api"}
	for _, raw := range []string{"https://app.example.com/api/health", "https://app.example.com/api"} {
		if !publishedURL(raw, routes) {
			t.Errorf("refused %s", raw)
		}
	}
	for _, raw := range []string{"http://app.example.com/api", "https://evil.example/api", "https://app.example.com:9000/api", "https://user:pass@app.example.com/api", "https://app.example.com/apix", "https://app.example.com/api/../admin", "https://app.example.com/api%2fadmin"} {
		if publishedURL(raw, routes) {
			t.Errorf("accepted %s", raw)
		}
	}
}
func TestMonitoringRoutesEnforceAbilitiesAndBoundBodies(t *testing.T) {
	a := platformAPI(t)
	user, _ := a.Identity.CreateUser(context.Background(), "Alice", "alice@example.com", "test-password-long")
	_, read, _ := a.Identity.CreateToken(context.Background(), user.ID, "read", []identity.Ability{identity.AbilityRead})
	_, write, _ := a.Identity.CreateToken(context.Background(), user.ID, "write", []identity.Ability{identity.AbilityWrite})
	mux := http.NewServeMux()
	a.registerMonitoringRoutes(mux)
	a.registerInstanceRoutes(mux)
	a.registerAuditRoutes(mux)
	for _, tc := range []struct {
		method, path, token, body string
		status                    int
	}{{"GET", "/api/v1/monitoring/options", read, "", 200}, {"PATCH", "/api/v1/monitoring/options", read, `{}`, 403}, {"PATCH", "/api/v1/monitoring/options", write, `{"disk":80,"memory":85,"cpu":90,"minutes":5}`, 200}, {"POST", "/api/v1/monitoring/silences", write, `{"until_at":"invalid"}`, 422}, {"POST", "/api/v1/monitoring/uptime", write, `{"resource":"missing","url":"http://localhost"}`, 404}, {"GET", "/api/v1/monitoring/uptime", read, "", 200}, {"GET", "/api/v1/instance/backups/config", read, "", 403}, {"POST", "/api/v1/instance/backups", read, `{}`, 403}, {"GET", "/api/v1/audit", write, "", 403}, {"PATCH", "/api/v1/monitoring/options", write, `{"unused":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413}} {
		r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		r.Header.Set("Authorization", "Bearer "+tc.token)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Errorf("%s %s: %d want %d: %s", tc.method, tc.path, w.Code, tc.status, w.Body.String())
		}
	}
}
func TestFrozenEnvironmentRequiresReasonToOverride(t *testing.T) {
	a := platformAPI(t)
	p, err := a.Projects.Create(context.Background(), "project", "")
	if err != nil {
		t.Fatal(err)
	}
	e := p.Environments[0]
	a.Projects.SetProtection(context.Background(), e.ID, true, true)
	for _, tc := range []struct {
		reason string
		ok     bool
	}{{"", false}, {"bad", false}, {"incident resolved", true}} {
		body, _ := json.Marshal(map[string]any{"protected": true, "frozen": false, "reason": tc.reason})
		r := httptest.NewRequest("PATCH", "/", strings.NewReader(string(body)))
		r.SetPathValue("id", e.ID)
		err = a.environmentProtection(httptest.NewRecorder(), r)
		if (err == nil) != tc.ok {
			t.Fatalf("reason %q err %v", tc.reason, err)
		}
	}
}

func TestMonitoringUptimeLifecycleBindsToExistingPublishedResource(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	p, err := a.Projects.Create(ctx, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	db := a.Instance.DB
	now := sqlite.FormatTime(time.Now())
	if _, err = db.Exec(`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv','local','local','/unused',?)`, now); err != nil {
		t.Fatal(err)
	}
	config := application.Config{}
	config.Network.Domains = []string{"https://app.example.com/api"}
	raw, _ := json.Marshal(config)
	if _, err = db.Exec(`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at) VALUES('app',?,'srv','app',?,'',?,?)`, p.Environments[0].ID, string(raw), now, now); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.registerMonitoringRoutes(mux)
	// Exercise the authenticated routing boundary and the real State store.
	user, _ := a.Identity.CreateUser(ctx, "Alice", "alice@example.com", "test-password-long")
	_, secret, _ := a.Identity.CreateToken(ctx, user.ID, "write", []identity.Ability{identity.AbilityWrite})
	send := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer "+secret)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	for _, badURL := range []string{"http://127.0.0.1/private", "https://app.example.com/admin"} {
		body, _ := json.Marshal(monitor.UptimeCheck{Resource: "app", URL: badURL, ExpectedStatus: 200, IntervalSeconds: 60, Enabled: true})
		if w := send("POST", "/api/v1/monitoring/uptime", string(body)); w.Code != 422 {
			t.Fatalf("accepted unpublished URL %s: %s", badURL, w.Body.String())
		}
	}
	w := send("POST", "/api/v1/monitoring/uptime", `{"resource":"app","url":"https://app.example.com/api/health","expected_status":200,"interval_seconds":60,"enabled":true}`)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body.String())
	}
	var check monitor.UptimeCheck
	if err = json.Unmarshal(w.Body.Bytes(), &check); err != nil || check.ID == "" {
		t.Fatalf("check %+v err %v", check, err)
	}
	if w = send("GET", "/api/v1/monitoring/uptime/"+check.ID+"/history", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("history %d %s", w.Code, w.Body.String())
	}
	silence, _ := json.Marshal(map[string]string{"resource": "app", "until_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if w = send("POST", "/api/v1/monitoring/silences", string(silence)); w.Code != 204 {
		t.Fatalf("silence %d %s", w.Code, w.Body.String())
	}
	if w = send("DELETE", "/api/v1/monitoring/uptime/"+check.ID, ""); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	if w = send("GET", "/api/v1/monitoring/uptime", ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("list %d %s", w.Code, w.Body.String())
	}
}

func TestAuditCoversRejectedAndUnknownMutationsWithoutReadingTheirBodies(t *testing.T) {
	a := platformAPI(t)
	mux := http.NewServeMux()
	a.route(mux, "POST /api/v1/test", canWrite, func(w http.ResponseWriter, r *http.Request) error { return noContent(w) })
	for _, path := range []string{"/api/v1/test", "/does-not-exist"} {
		r := httptest.NewRequest("POST", path, strings.NewReader(`{"password":"dummy"}`))
		r.Header.Set("Content-Type", "application/json")
		a.auditRequest(mux).ServeHTTP(httptest.NewRecorder(), r)
	}
	entries, err := a.Audit.List(context.Background(), audit.Filter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %+v err %v", entries, err)
	}
	if entries[0].Status != 404 || entries[1].Status != 401 || entries[1].Summary != "" {
		t.Fatalf("audit %+v", entries)
	}
}

func TestUnfreezeReasonIsAuditedEvenWhenAnUnusedFieldExceedsCaptureLimit(t *testing.T) {
	a := platformAPI(t)
	ctx := context.Background()
	p, err := a.Projects.Create(ctx, "Test", "")
	if err != nil {
		t.Fatal(err)
	}
	e := p.Environments[0]
	if _, err = a.Projects.SetProtection(ctx, e.ID, false, true); err != nil {
		t.Fatal(err)
	}
	user, _ := a.Identity.CreateUser(ctx, "Alice", "alice@example.com", "test-password-long")
	_, secret, _ := a.Identity.CreateToken(ctx, user.ID, "write", []identity.Ability{identity.AbilityWrite})
	body, _ := json.Marshal(map[string]any{"unused": strings.Repeat("x", 10000), "protected": false, "frozen": false, "reason": "incident resolved"})
	mux := http.NewServeMux()
	a.registerProjectRoutes(mux)
	r := httptest.NewRequest("PATCH", "/api/v1/environments/"+e.ID+"/protection", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+secret)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.auditRequest(mux).ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	entries, err := a.Audit.List(ctx, audit.Filter{})
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Summary, "incident resolved") {
		t.Fatalf("entries %+v err %v", entries, err)
	}
}

func TestNestedMCPAuditRetainsCallerAddress(t *testing.T) {
	a := platformAPI(t)
	nested := a.auditRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	outer := a.auditRequest(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		child := httptest.NewRequest("POST", "/api/v1/operation", nil).WithContext(r.Context())
		child.RemoteAddr = "127.0.0.1:0"
		nested.ServeHTTP(httptest.NewRecorder(), child)
		w.WriteHeader(204)
	}))
	r := httptest.NewRequest("POST", "/api/v1/origin", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	outer.ServeHTTP(httptest.NewRecorder(), r)
	entries, err := a.Audit.List(context.Background(), audit.Filter{})
	if err != nil || len(entries) != 2 {
		t.Fatalf("entries %+v err %v", entries, err)
	}
	for _, e := range entries {
		if e.IP != "203.0.113.9" {
			t.Fatalf("lost caller address: %+v", e)
		}
	}
	if sensitiveRead("/api/v1/instance/backups/status") {
		t.Fatal("backup status polling floods audit")
	}
	if !sensitiveRead("/api/v1/settings/backup-s3") {
		t.Fatal("backup configuration read not audited")
	}
}
