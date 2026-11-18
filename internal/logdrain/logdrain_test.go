package logdrain

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const secretToken = "drain-token-0123456789"

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, query := range []string{
		`INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv','QA','local','/qa','2026-01-01')`,
		`INSERT INTO projects(id,name,created_at) VALUES('p','Loja','2026-01-01')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES('e','p','prod','2026-01-01')`,
		`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at) VALUES('app','e','srv','api','{}','s','2026-01-01','2026-01-01')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

type fakeConnector struct{ ex executor.Executor }

func (c fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "srv"}, nil
}

type replacer struct{ secret string }

func (r replacer) Mask(text string) string {
	return strings.ReplaceAll(text, r.secret, "••••••")
}

type capture struct {
	mu       sync.Mutex
	bodies   []string
	auth     []string
	paths    []string
	failWith int
}

func (c *capture) server(t *testing.T) *httptest.Server {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		defer c.mu.Unlock()
		c.bodies, c.auth, c.paths = append(c.bodies, string(body)), append(c.auth, r.Header.Get("Authorization")), append(c.paths, r.URL.Path)
		if c.failWith != 0 {
			w.WriteHeader(c.failWith)
			_, _ = io.WriteString(w, "rejected "+secretToken)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func (c *capture) count() int { c.mu.Lock(); defer c.mu.Unlock(); return len(c.bodies) }

func newTestService(t *testing.T, db *sql.DB, ex executor.Executor, redactors RedactorSource) *Service {
	box := testbox.Box(t)
	service := NewService(db, box, fakeConnector{ex}, redactors, slog.New(slog.NewTextHandler(io.Discard, nil)))
	service.forwarder.retries = []time.Duration{time.Millisecond}
	return service
}

func token(value string) *string { return &value }

func TestDrainInputValidation(t *testing.T) {
	valid := Input{Name: "loki", Kind: KindLoki, URL: "https://logs.example.com", ScopeType: ScopeProject, ScopeID: "p"}
	if _, err := valid.normalize(false); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]Input{
		"token over http":    {Name: "x", Kind: KindHTTP, URL: "http://logs.example.com", Token: token(secretToken), ScopeType: ScopeProject, ScopeID: "p"},
		"credentials in url": {Name: "x", Kind: KindHTTP, URL: "https://u:p@logs.example.com", ScopeType: ScopeProject, ScopeID: "p"},
		"reserved label":     {Name: "x", Kind: KindHTTP, URL: "https://l.example.com", Labels: map[string]string{"pier_server": "x"}, ScopeType: ScopeProject, ScopeID: "p"},
		"bad label":          {Name: "x", Kind: KindHTTP, URL: "https://l.example.com", Labels: map[string]string{"a-b": "x"}, ScopeType: ScopeProject, ScopeID: "p"},
		"file url":           {Name: "x", Kind: KindHTTP, URL: "file:///etc/passwd", ScopeType: ScopeProject, ScopeID: "p"},
		"syslog kind":        {Name: "x", Kind: "syslog", URL: "https://l.example.com", ScopeType: ScopeProject, ScopeID: "p"},
		"database scope":     {Name: "x", Kind: KindHTTP, URL: "https://l.example.com", ScopeType: "database", ScopeID: "p"},
		"token with space":   {Name: "x", Kind: KindHTTP, URL: "https://l.example.com", Token: token("a b"), ScopeType: ScopeProject, ScopeID: "p"},
	} {
		if _, err := input.normalize(false); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	keptToken := Input{Name: "x", Kind: KindHTTP, URL: "http://logs.internal:8080/in", ScopeType: ScopeProject, ScopeID: "p"}
	if _, err := keptToken.normalize(true); err == nil {
		t.Fatal("stored token would travel over http")
	}
}

func TestDrainLifecycleSealsTokenAndRequiresTypedName(t *testing.T) {
	db := testDB(t)
	service := newTestService(t, db, executor.NewFake(), nil)
	ctx := context.Background()
	if _, err := service.Create(ctx, Input{Name: "x", Kind: KindHTTP, URL: "https://l.example.com", ScopeType: ScopeApplication, ScopeID: "missing"}); err == nil {
		t.Fatal("missing scope accepted")
	}
	view, err := service.Create(ctx, Input{Name: "central", Kind: KindHTTP, URL: "https://l.example.com/in", Token: token(secretToken),
		Labels: map[string]string{"env": "prod"}, ScopeType: ScopeProject, ScopeID: "p", Enabled: true})
	if err != nil || !view.HasToken {
		t.Fatalf("%+v %v", view, err)
	}
	encoded, _ := json.Marshal(view)
	var stored string
	_ = db.QueryRow(`SELECT token FROM log_drains`).Scan(&stored)
	if strings.Contains(string(encoded), secretToken) || strings.Contains(stored, secretToken) || stored == "" {
		t.Fatalf("token exposed: %s / %s", encoded, stored)
	}
	view, err = service.Update(ctx, view.ID, Input{Name: "central", Kind: KindHTTP, URL: "https://l.example.com/v2", ScopeType: ScopeProject, ScopeID: "p"})
	if err != nil || !view.HasToken || view.Enabled {
		t.Fatalf("nil token must keep the stored one: %+v %v", view, err)
	}
	view, err = service.Update(ctx, view.ID, Input{Name: "central", Kind: KindHTTP, URL: "http://l.internal/v2", Token: token(""), ScopeType: ScopeProject, ScopeID: "p"})
	if err != nil || view.HasToken {
		t.Fatalf("empty token must clear it: %+v %v", view, err)
	}
	if _, err := service.Create(ctx, Input{Name: "central", Kind: KindLoki, URL: "https://l.example.com", ScopeType: ScopeProject, ScopeID: "p"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := service.Delete(ctx, view.ID, "centra"); err == nil {
		t.Fatal("wrong confirmation deleted")
	}
	if err := service.Delete(ctx, view.ID, "central"); err != nil {
		t.Fatal(err)
	}
}

func TestHTTPAndLokiSinksSpeakTheirFormats(t *testing.T) {
	received := &capture{}
	endpoint := received.server(t)
	at := time.Date(2026, 11, 12, 10, 0, 0, 5, time.UTC)
	records := []Record{{Time: at, Message: "a", Labels: map[string]string{"x": "1"}}, {Time: at, Message: "b", Labels: map[string]string{"x": "2"}}, {Time: at, Message: "c", Labels: map[string]string{"x": "1"}}}
	if err := newSink(Drain{Kind: KindHTTP, URL: endpoint.URL + "/in"}, secretToken, endpoint.Client()).Send(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	var array []Record
	if json.Unmarshal([]byte(received.bodies[0]), &array) != nil || len(array) != 3 || received.auth[0] != "Bearer "+secretToken {
		t.Fatalf("http body %s auth %q", received.bodies[0], received.auth[0])
	}
	if err := newSink(Drain{Kind: KindLoki, URL: endpoint.URL}, "123456:glc_key", endpoint.Client()).Send(context.Background(), records); err != nil {
		t.Fatal(err)
	}
	var push struct {
		Streams []struct {
			Stream map[string]string
			Values [][2]string
		}
	}
	if err := json.Unmarshal([]byte(received.bodies[1]), &push); err != nil || len(push.Streams) != 2 || len(push.Streams[0].Values) != 2 ||
		push.Streams[0].Values[0][0] != "1794477600000000005" || received.paths[1] != "/loki/api/v1/push" || !strings.HasPrefix(received.auth[1], "Basic ") {
		t.Fatalf("loki push %s path %s auth %s", received.bodies[1], received.paths[1], received.auth[1])
	}
	received.failWith = http.StatusInternalServerError
	err := newSink(Drain{Kind: KindHTTP, URL: endpoint.URL}, secretToken, endpoint.Client()).Send(context.Background(), records)
	if err == nil || strings.Contains(err.Error(), secretToken) {
		t.Fatalf("failure leaked token: %v", err)
	}
}

func TestBufferIsBoundedAndCountsDrops(t *testing.T) {
	buffer := newBuffer(3)
	for index := 0; index < 5; index++ {
		buffer.push(Record{Message: string(rune('a' + index))})
	}
	batch := buffer.take(10)
	if len(batch) != 3 || batch[0].Message != "c" || batch[2].Message != "e" || buffer.snapshot().Dropped != 2 || buffer.snapshot().Buffered != 0 {
		t.Fatalf("batch %+v status %+v", batch, buffer.snapshot())
	}
	buffer.failed(4, "HTTP 500", time.Now())
	if status := buffer.snapshot(); status.Dropped != 6 || status.FailedBatches != 1 || status.LastError != "HTTP 500" {
		t.Fatalf("%+v", status)
	}
}

const psLine = "c0ffee\tapi-1\tapi:latest\trunning\tUp 1h\tapplication\tapp\td1"

func TestForwarderShipsMaskedLabeledLinesAndSurvivesFailures(t *testing.T) {
	db := testDB(t)
	received := &capture{}
	endpoint := received.server(t)
	now := time.Now().UTC()
	ex := executor.NewFake().On("docker ps", psLine, 0).
		On("docker logs", hexLogs(now.Add(time.Second).Format(time.RFC3339Nano)+" password=hunter2-secret\n"+now.Add(2*time.Second).Format(time.RFC3339Nano)+" ready\n"), 0)
	redactorFails := false
	service := newTestService(t, db, ex, func(_ context.Context, kind, id string) (Masker, error) {
		if redactorFails {
			return nil, errors.New("env unreadable")
		}
		return replacer{"hunter2-secret"}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer service.forwarder.stopAll()
	service.SetHTTPClient(endpoint.Client())
	view, err := service.Create(ctx, Input{Name: "central", Kind: KindHTTP, URL: endpoint.URL, Token: token(secretToken), Labels: map[string]string{"env": "prod"}, ScopeType: ScopeProject, ScopeID: "p", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.forwarder.cycle(ctx); err != nil || ex.Ran("docker logs") {
		t.Fatalf("first cycle must only place the cursor: %v %v", err, ex.Commands)
	}
	if err := service.forwarder.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return received.count() == 1 })
	body := received.bodies[0]
	if strings.Contains(body, "hunter2-secret") || !strings.Contains(body, "••••••") || !strings.Contains(body, `"env":"prod"`) ||
		!strings.Contains(body, `"pier_resource_id":"app"`) || !strings.Contains(body, `"pier_container":"api-1"`) {
		t.Fatalf("body %s", body)
	}
	if !ex.Ran("docker logs --timestamps --since") || !ex.Ran("| od -An -v -tx1") {
		t.Fatalf("commands %v", ex.Commands)
	}
	waitFor(t, func() bool { return service.forwarder.Status(view.ID).Sent == 2 })

	received.mu.Lock()
	received.failWith = http.StatusServiceUnavailable
	received.mu.Unlock()
	ex.On("docker logs", hexLogs(now.Add(time.Hour).Format(time.RFC3339Nano)+" later\n"), 0)
	if err := service.forwarder.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return service.forwarder.Status(view.ID).FailedBatches == 1 })
	status := service.forwarder.Status(view.ID)
	if status.Dropped != 1 || strings.Contains(status.LastError, secretToken) || !strings.Contains(status.LastError, "503") {
		t.Fatalf("status %+v", status)
	}

	redactorFails = true
	ex.On("docker logs", hexLogs(now.Add(2*time.Hour).Format(time.RFC3339Nano)+" password=hunter2-secret\n"), 0)
	if err := service.forwarder.cycle(ctx); err != nil {
		t.Fatal(err)
	}
	if service.forwarder.Status(view.ID).Dropped != 2 {
		t.Fatalf("unmaskable lines must be dropped: %+v", service.forwarder.Status(view.ID))
	}
	if strings.Contains(strings.Join(received.bodies, ""), "hunter2-secret") {
		t.Fatal("secret reached the sink")
	}
}

func TestDrainTestSendsOneRecord(t *testing.T) {
	db := testDB(t)
	received := &capture{}
	endpoint := received.server(t)
	service := newTestService(t, db, executor.NewFake(), nil)
	service.SetHTTPClient(endpoint.Client())
	view, err := service.Create(context.Background(), Input{Name: "t", Kind: KindLoki, URL: endpoint.URL, ScopeType: ScopeApplication, ScopeID: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Test(context.Background(), view.ID); err != nil || received.count() != 1 || !strings.Contains(received.bodies[0], "pier_test") {
		t.Fatalf("%v %v", err, received.bodies)
	}
	received.failWith = http.StatusUnauthorized
	if err := service.Test(context.Background(), view.ID); err == nil {
		t.Fatal("rejection hidden")
	}
}

func waitFor(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func hexLogs(raw string) string {
	var out strings.Builder
	for _, b := range []byte(raw + "PIER_LOG_EXIT=000") {
		out.WriteString(hex.EncodeToString([]byte{b}))
		out.WriteByte(32)
	}
	return out.String()
}
