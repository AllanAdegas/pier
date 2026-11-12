package audit

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/sqlite"
)

func TestAuditSummaryNeverPersistsSecretsOrMalformedInput(t *testing.T) {
	raw := []byte(`{"name":"deploy","reason":"maintenance","password":"dummy1","nested":{"API_TOKEN":"dummy2","values":["dummy3"]},"current":"dummy4","next":"dummy5","setup_code":"dummy6","compose":"dummy7"}`)
	got := Summary(raw, false)
	if strings.Contains(got, "dummy") {
		t.Fatalf("secret in %s", got)
	}
	if !strings.Contains(got, "maintenance") {
		t.Fatal("lost reason")
	}
	got = Summary([]byte(`{"variables":{"CUSTOM":"dummy"},"key":"API","value":"dummy"}`), true)
	if strings.Contains(got, "dummy") {
		t.Fatalf("environment secret in %s", got)
	}
	if got = Summary([]byte(`{"password":"dummy`), false); strings.Contains(got, "dummy") {
		t.Fatal("truncated input persisted")
	}
}
func TestAuditFiltersPaginationAndRetention(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := Store{DB: db}
	old := sqlite.FormatTime(time.Now().Add(-91 * 24 * time.Hour))
	if err = s.Record(ctx, Entry{OccurredAt: old, Method: "POST", IP: "127.0.0.1", Route: "POST /old"}); err != nil {
		t.Fatal(err)
	}
	for _, user := range []string{"alice", "bob", "alice"} {
		if err = s.Record(ctx, Entry{OccurredAt: sqlite.FormatTime(time.Now()), UserID: user, TokenID: "ci", Method: "POST", IP: "127.0.0.1", Route: "POST /route", ResourceID: "app", Status: 201}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.List(ctx, Filter{UserID: "alice", TokenID: "ci", ResourceID: "app", Method: "POST", Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows %+v %v", rows, err)
	}
	next, err := s.List(ctx, Filter{UserID: "alice", Before: rows[0].ID})
	if err != nil || len(next) != 1 || next[0].ID >= rows[0].ID {
		t.Fatalf("pagination %+v %v", next, err)
	}
	all, err := s.List(ctx, Filter{})
	if err != nil || len(all) != 3 {
		t.Fatalf("retention %+v %v", all, err)
	}
}
