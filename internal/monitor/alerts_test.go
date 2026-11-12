package monitor

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"pier/internal/notify"
	"pier/internal/platform/sqlite"
)

type recorder struct{ events []notify.Event }

func (r *recorder) Publish(e notify.Event) { r.events = append(r.events, e) }
func testState(t *testing.T) *State {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &State{DB: db}
}
func TestAlertTransitionsAndSilences(t *testing.T) {
	ctx := context.Background()
	state := testState(t)
	publisher := &recorder{}
	now := time.Now().UTC()
	event := notify.Event{Type: notify.UptimeDown, Resource: "app:check"}
	observe := func(active bool, at time.Time) {
		t.Helper()
		if err := state.Observe(ctx, publisher, event, active, at); err != nil {
			t.Fatal(err)
		}
	}
	observe(true, now)
	observe(true, now.Add(time.Hour))
	if len(publisher.events) != 1 {
		t.Fatal("duplicate active alert")
	}
	// State survives reconstruction and re-notifies at most every six hours.
	state = &State{DB: state.DB}
	observe(true, now.Add(6*time.Hour))
	if len(publisher.events) != 2 {
		t.Fatal("missing reminder")
	}
	if err := state.Silence(ctx, "app", now.Add(8*time.Hour)); err != nil {
		t.Fatal(err)
	}
	observe(false, now.Add(7*time.Hour))
	if len(publisher.events) != 2 {
		t.Fatal("resource silence ignored")
	}
	observe(false, now.Add(9*time.Hour))
	observe(false, now.Add(10*time.Hour))
	if len(publisher.events) != 3 || publisher.events[2].Type != notify.AlertResolved {
		t.Fatal("missing single recovery")
	}
	if err := state.Silence(ctx, "", now.Add(12*time.Hour)); err != nil {
		t.Fatal(err)
	}
	observe(true, now.Add(11*time.Hour))
	observe(true, now.Add(13*time.Hour))
	if len(publisher.events) != 4 {
		t.Fatal("global silence lost transition")
	}
}
func TestUptimeHistoryAndRecovery(t *testing.T) {
	ctx := context.Background()
	state := testState(t)
	publisher := &recorder{}
	status := http.StatusServiceUnavailable
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) }))
	defer server.Close()
	c, err := state.SaveUptime(ctx, UptimeCheck{Resource: "app", URL: server.URL, ExpectedStatus: 200, IntervalSeconds: 60, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	m := &Monitor{Alerts: state, Events: publisher, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	m.checkUptime(ctx)
	m.checkUptime(ctx)
	history, err := state.UptimeHistory(ctx, c.ID)
	if err != nil || len(history) != 1 || history[0].Healthy {
		t.Fatalf("bad history: %v %v", history, err)
	}
	if len(publisher.events) != 1 {
		t.Fatal("expected failure")
	}
	status = 200
	if _, err := state.DB.Exec(`UPDATE uptime_checks SET last_checked=''`); err != nil {
		t.Fatal(err)
	}
	m.checkUptime(ctx)
	if len(publisher.events) != 2 || publisher.events[1].Type != notify.AlertResolved {
		t.Fatal("missing recovery")
	}
	if err := state.DeleteUptime(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	history, err = state.UptimeHistory(ctx, c.ID)
	if err != nil || len(history) != 0 {
		t.Fatal("history not cascaded")
	}
}
func TestUptimeValidation(t *testing.T) {
	s := testState(t)
	for _, url := range []string{"file:///etc/passwd", "https://user:pass@example.com", "http://"} {
		if _, err := s.SaveUptime(context.Background(), UptimeCheck{Resource: "app", URL: url, ExpectedStatus: 200, IntervalSeconds: 60}); err == nil {
			t.Fatalf("accepted %s", url)
		}
	}
	if _, err := s.SaveUptime(context.Background(), UptimeCheck{Resource: "app", URL: "https://example.com", ExpectedStatus: 200, IntervalSeconds: 59}); err == nil {
		t.Fatal("accepted short interval")
	}
	if err := s.SetOptions(context.Background(), Options{Disk: 101, Memory: 90, CPU: 95, Minutes: 10}); err == nil {
		t.Fatal("accepted invalid threshold")
	}
	o, err := s.Options(context.Background())
	if err != nil || o.Disk != 90 || o.CPU != 95 || o.Minutes != 10 {
		t.Fatalf("defaults: %+v %v", o, err)
	}
}
func TestMissedSchedule(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if missedSchedule("0 * * * *", base, nil, base.Add(69*time.Minute)) {
		t.Fatal("ignored grace")
	}
	if !missedSchedule("0 * * * *", base, nil, base.Add(71*time.Minute)) {
		t.Fatal("missing silence alarm")
	}
	last := base.Add(time.Hour)
	if missedSchedule("0 * * * *", base, &last, base.Add(71*time.Minute)) {
		t.Fatal("ignored success")
	}
	if missedSchedule("bad", base, nil, base.Add(24*time.Hour)) {
		t.Fatal("invalid cron")
	}
}
func TestCertificateChecks(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	bad, err := certificateIssue(ctx, server.URL, time.Now())
	if !bad || err == nil {
		t.Fatal("self-signed certificate accepted")
	}
	bad, err = certificateIssue(ctx, "http://example.com", time.Now())
	if bad || err != nil {
		t.Fatal("checked plaintext route")
	}
}

func TestRestartWindowUsesDeltasAndExpires(t *testing.T) {
	now := time.Now()
	samples, active := restartWindow(nil, now, 100, 20*time.Second)
	if active {
		t.Fatal("lifetime counter triggered alert")
	}
	samples, active = restartWindow(samples, now.Add(time.Minute), 102, 20*time.Second)
	if active {
		t.Fatal("two restarts triggered loop")
	}
	samples, active = restartWindow(samples, now.Add(2*time.Minute), 103, 20*time.Second)
	if !active {
		t.Fatal("three restarts were missed")
	}
	samples, active = restartWindow(samples, now.Add(13*time.Minute), 103, 20*time.Second)
	if active {
		t.Fatal("old restarts still active")
	}
	_, active = restartWindow(samples, now.Add(14*time.Minute), 0, 20*time.Second)
	if active {
		t.Fatal("counter reset triggered loop")
	}
}

func TestThresholdRequiresContinuousHighSamples(t *testing.T) {
	now := time.Now()
	since, active := sustainedThreshold(time.Time{}, now, true, 10*time.Minute)
	if active {
		t.Fatal("instant threshold fired")
	}
	since, active = sustainedThreshold(since, now.Add(9*time.Minute), true, 10*time.Minute)
	if active {
		t.Fatal("threshold fired before duration")
	}
	_, active = sustainedThreshold(since, now.Add(10*time.Minute), true, 10*time.Minute)
	if !active {
		t.Fatal("sustained threshold missed")
	}
	since, active = sustainedThreshold(since, now.Add(11*time.Minute), false, 10*time.Minute)
	if active || !since.IsZero() {
		t.Fatal("recovery did not reset timer")
	}
	_, active = sustainedThreshold(since, now.Add(12*time.Minute), true, 10*time.Minute)
	if active {
		t.Fatal("new spike reused old timer")
	}
}
