package updates

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c *clock) Now() time.Time          { return c.now }
func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

type fakeGitHub struct {
	hits      atomic.Int32
	userAgent atomic.Value
	headers   atomic.Value
	status    int
	body      string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.hits.Add(1)
	f.userAgent.Store(r.UserAgent())
	f.headers.Store(r.Header.Clone())
	w.WriteHeader(f.status)
	_, _ = w.Write([]byte(f.body))
}

const releaseBody = `{"tag_name":"v0.6.0","html_url":"https://github.com/AllanAdegas/pier/releases/tag/v0.6.0",` +
	`"body":"## Features\n- previews","draft":false,"prerelease":false}`

func newChecker(t *testing.T, github *fakeGitHub, enabled bool) (*Checker, *clock) {
	t.Helper()
	server := httptest.NewServer(github)
	t.Cleanup(server.Close)
	clk := &clock{now: time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)}
	checker, err := New(Options{Enabled: enabled, Current: "0.5.0", URL: server.URL, Now: clk.Now,
		Timeout: 200 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return checker, clk
}

func TestStatusReportsNewerRelease(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, clk := newChecker(t, github, true)

	status := checker.Status(context.Background())

	if !status.Enabled || !status.UpdateAvailable || status.Current != "0.5.0" || status.Latest != "0.6.0" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if status.URL != "https://github.com/AllanAdegas/pier/releases/tag/v0.6.0" || !strings.Contains(status.Notes, "previews") {
		t.Fatalf("unexpected url/notes: %+v", status)
	}
	if status.CheckedAt == nil || !status.CheckedAt.Equal(clk.now) {
		t.Fatalf("checked_at: %v", status.CheckedAt)
	}
}

func TestStatusJSONShape(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, _ := newChecker(t, github, true)
	raw, err := json.Marshal(checker.Status(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for _, key := range []string{"enabled", "current", "latest", "update_available", "url", "notes", "checked_at"} {
		if _, found := fields[key]; !found {
			t.Errorf("missing key %q in %s", key, raw)
		}
	}
}

func TestCacheHoldsUntilIntervalPasses(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, clk := newChecker(t, github, true)

	checker.Status(context.Background())
	clk.advance(11*time.Hour + 59*time.Minute)
	checker.Status(context.Background())
	if hits := github.hits.Load(); hits != 1 {
		t.Fatalf("hits before the interval = %d, want 1", hits)
	}
	clk.advance(2 * time.Minute)
	checker.Status(context.Background())
	if hits := github.hits.Load(); hits != 2 {
		t.Fatalf("hits after the interval = %d, want 2", hits)
	}
}

func TestDisabledNeverUsesTheNetwork(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, _ := newChecker(t, github, false)

	status := checker.Status(context.Background())

	if github.hits.Load() != 0 {
		t.Fatal("a disabled checker contacted GitHub")
	}
	if status.Enabled || status.UpdateAvailable || status.Current != "0.5.0" || status.CheckedAt != nil {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestNilCheckerIsDisabled(t *testing.T) {
	var checker *Checker
	if status := checker.Status(context.Background()); status.Enabled {
		t.Fatalf("nil checker enabled: %+v", status)
	}
}

func TestEnabledByEnvironment(t *testing.T) {
	for value, want := range map[string]bool{"": true, "1": true, "0": false, " 0 ": false, "false": true} {
		if got := EnabledByEnvironment(value); got != want {
			t.Errorf("EnabledByEnvironment(%q) = %v, want %v", value, got, want)
		}
	}
}

func TestRequestIsAnonymous(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, _ := newChecker(t, github, true)
	checker.Status(context.Background())

	if agent := github.userAgent.Load(); agent != UserAgent {
		t.Fatalf("User-Agent = %v", agent)
	}
	headers := github.headers.Load().(http.Header)
	for _, name := range []string{"Authorization", "Cookie", "Referer", "X-Forwarded-For"} {
		if headers.Get(name) != "" {
			t.Errorf("header %s was sent", name)
		}
	}
}

func TestTimeoutKeepsPreviousAnswerAndCountsAsAttempt(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	clk := &clock{now: time.Now()}
	checker, err := New(Options{Enabled: true, Current: "0.5.0", URL: slow.URL, Now: clk.Now, Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	status := checker.Status(context.Background())

	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("the timeout did not apply: %v", elapsed)
	}
	if status.UpdateAvailable || status.CheckedAt != nil || !status.Enabled {
		t.Fatalf("unexpected status after timeout: %+v", status)
	}
	if checker.due() {
		t.Fatal("a failed attempt must still wait for the interval")
	}
}

func TestFailureKeepsLastGoodAnswer(t *testing.T) {
	github := &fakeGitHub{status: 200, body: releaseBody}
	checker, clk := newChecker(t, github, true)
	checker.Status(context.Background())

	github.status, github.body = 500, "boom"
	clk.advance(DefaultInterval)
	status := checker.Status(context.Background())

	if status.Latest != "0.6.0" || !status.UpdateAvailable {
		t.Fatalf("lost the previous answer: %+v", status)
	}
}

func TestRejectsUnusableAnswers(t *testing.T) {
	cases := map[string]string{
		"draft":      `{"tag_name":"v9.0.0","draft":true}`,
		"prerelease": `{"tag_name":"v9.0.0","prerelease":true}`,
		"bad tag":    `{"tag_name":"latest"}`,
		"not json":   `<html>`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			checker, _ := newChecker(t, &fakeGitHub{status: 200, body: body}, true)
			if status := checker.Status(context.Background()); status.UpdateAvailable || status.Latest != "" {
				t.Fatalf("accepted %s: %+v", name, status)
			}
		})
	}
}

func TestForeignReleaseURLIsReplaced(t *testing.T) {
	body := `{"tag_name":"v0.6.0","html_url":"https://evil.example/x"}`
	checker, _ := newChecker(t, &fakeGitHub{status: 200, body: body}, true)
	status := checker.Status(context.Background())
	if status.URL != "https://github.com/AllanAdegas/pier/releases/tag/v0.6.0" {
		t.Fatalf("url = %q", status.URL)
	}
}

func TestSameOrOlderReleaseIsNoUpdate(t *testing.T) {
	for _, tag := range []string{"v0.5.0", "v0.4.9"} {
		body := `{"tag_name":"` + tag + `"}`
		checker, _ := newChecker(t, &fakeGitHub{status: 200, body: body}, true)
		if status := checker.Status(context.Background()); status.UpdateAvailable || status.Latest == "" {
			t.Fatalf("%s: %+v", tag, status)
		}
	}
}

func TestNotesExcerptIsBounded(t *testing.T) {
	body, _ := json.Marshal(map[string]string{"tag_name": "v0.6.0", "body": strings.Repeat("é", 5000)})
	checker, _ := newChecker(t, &fakeGitHub{status: 200, body: string(body)}, true)
	notes := checker.Status(context.Background()).Notes
	if got := len([]rune(notes)); got != maxNotesRunes+1 {
		t.Fatalf("notes runes = %d", got)
	}
}

func TestNewRejectsInvalidCurrentVersion(t *testing.T) {
	if _, err := New(Options{Enabled: true, Current: "dev"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		latest, current string
		newer           bool
	}{
		{"0.6.0", "0.5.0", true},
		{"v1.0.0", "0.99.99", true},
		{"0.5.1", "0.5.0", true},
		{"0.5.0", "0.5.0", false},
		{"0.4.0", "0.5.0", false},
		{"0.10.0", "0.9.0", true},
		{"0.5.0", "0.5.0-rc.1", true},
		{"0.5.0-rc.1", "0.5.0", false},
		{"0.5.0+build", "0.5.0", false},
	}
	for _, c := range cases {
		latest, err := parseRelease(c.latest)
		if err != nil {
			t.Fatal(err)
		}
		current, err := parseRelease(c.current)
		if err != nil {
			t.Fatal(err)
		}
		if got := latest.newerThan(current); got != c.newer {
			t.Errorf("%s newer than %s = %v, want %v", c.latest, c.current, got, c.newer)
		}
	}
	for _, bad := range []string{"", "1.2", "a.b.c", "1.2.3.4", "1.2.-3", "v1.2.3 "} {
		if _, err := parseRelease(bad); err == nil {
			t.Errorf("parseRelease(%q) accepted", bad)
		}
	}
}
