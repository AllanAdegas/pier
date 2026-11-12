package gitprovider

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestRepositoryTokensAreNarrowedAndSeparatedByPurpose(t *testing.T) {
	s := fixture(t)
	calls := 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var input struct {
			Repositories []string          `json:"repositories"`
			Permissions  map[string]string `json:"permissions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Fatal(err)
		}
		if len(input.Repositories) != 1 || input.Repositories[0] != "app" {
			t.Fatal(input)
		}
		if calls == 1 && (len(input.Permissions) != 1 || input.Permissions["contents"] != "read") {
			t.Fatal(input)
		}
		if calls == 2 && (len(input.Permissions) != 2 || input.Permissions["statuses"] != "write" || input.Permissions["contents"] != "") {
			t.Fatal(input)
		}
		json.NewEncoder(w).Encode(map[string]any{"token": "scoped", "expires_at": time.Now().Add(time.Hour)})
	})
	for _, reporting := range []bool{false, false, true, true} {
		s.mu.Lock()
		_, err := s.repositoryToken(t.Context(), 123, "acme/app", reporting)
		s.mu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatal(calls)
	}
	if _, err := s.db.Exec(`INSERT INTO git_provider_installations(id,suspended) VALUES(123,1)`); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	_, err := s.repositoryToken(t.Context(), 123, "acme/app", false)
	s.mu.Unlock()
	if err == nil || calls != 2 {
		t.Fatal("suspended cached token accepted")
	}
}

func TestReporterDoesNotEditAnotherAppsMarkerComment(t *testing.T) {
	s := fixture(t)
	s.scopedTokens = map[string]cachedToken{"123/owner/repo/true": {"token", time.Now().Add(time.Hour)}}
	created, patched := 0, 0
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET":
			w.Write([]byte(`[{"id":5,"body":"<!-- pier:preview -->\nother app","user":{"type":"Bot"},"performed_via_github_app":{"id":99}}]`))
		case r.Method == "PATCH":
			patched++
		case r.Method == "POST" && strings.Contains(r.URL.Path, "/comments"):
			created++
		}
	})
	if err := s.Report(t.Context(), Selection{123, "owner/repo"}, strings.Repeat("a", 40), 3, "success", "ready"); err != nil {
		t.Fatal(err)
	}
	if patched != 0 || created != 1 {
		t.Fatal(patched, created)
	}
}

func TestClearSelectionAndMissingSelectionSentinel(t *testing.T) {
	s := fixture(t)
	called := false
	_, err := s.db.Exec(`INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository) VALUES('application','app',123,'owner/repo')`)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.ClearSelection(t.Context(), "application", "app"); err != nil {
		t.Fatal(err)
	}
	err = s.WithCredentials(t.Context(), "application", "app", func(string, string) error { called = true; return nil })
	if !errors.Is(err, ErrNoSelection) || called {
		t.Fatal(err, called)
	}
}
