package gitprovider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTokensAreCachedAndRefreshedBeforeExpiry(t *testing.T) {
	s := fixture(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	mints := 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/123/access_tokens": func(w http.ResponseWriter, r *http.Request) {
			mints++
			json.NewEncoder(w).Encode(map[string]any{"token": "token-" + string(rune('0'+mints)), "expires_at": now.Add(time.Hour)})
		},
	})
	key := tokenKey{installation: 123, repository: "acme/app", purpose: purposeClone}
	first, err := s.installationToken(t.Context(), key)
	if err != nil || first != "token-1" {
		t.Fatal(first, err)
	}
	now = now.Add(50 * time.Minute)
	if again, _ := s.installationToken(t.Context(), key); again != "token-1" || mints != 1 {
		t.Fatal("token not reused before the renewal margin", again, mints)
	}
	now = now.Add(6 * time.Minute) // 4 minutes left: inside the renewal margin
	refreshed, err := s.installationToken(t.Context(), key)
	if err != nil || refreshed != "token-2" || mints != 2 {
		t.Fatal("token not refreshed", refreshed, mints, err)
	}
}

func TestTokensAreNarrowedAndSeparatedByPurpose(t *testing.T) {
	s := fixture(t)
	var requests []map[string]any
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/123/access_tokens": func(w http.ResponseWriter, r *http.Request) {
			var input map[string]any
			json.NewDecoder(r.Body).Decode(&input)
			requests = append(requests, input)
			json.NewEncoder(w).Encode(map[string]any{"token": "scoped", "expires_at": time.Now().Add(time.Hour)})
		},
	})
	for _, purpose := range []tokenPurpose{purposeClone, purposeClone, purposeReport, purposeReport, purposeList} {
		if _, err := s.installationToken(t.Context(), tokenKey{installation: 123, repository: "Acme/App", purpose: purpose}); err != nil {
			t.Fatal(err)
		}
	}
	if len(requests) != 3 {
		t.Fatal(len(requests))
	}
	clone, report, list := requests[0], requests[1], requests[2]
	if permissions(clone) != "contents=read" || repositories(clone) != "app" {
		t.Fatal(clone)
	}
	if permissions(report) != "pull_requests=write,statuses=write" || repositories(report) != "app" {
		t.Fatal(report)
	}
	if permissions(list) != "metadata=read" || list["repositories"] != nil {
		t.Fatal(list)
	}
}

func permissions(input map[string]any) string {
	values := []string{}
	for _, key := range []string{"contents", "metadata", "pull_requests", "statuses"} {
		if value, found := input["permissions"].(map[string]any)[key]; found {
			values = append(values, key+"="+value.(string))
		}
	}
	return strings.Join(values, ",")
}

func repositories(input map[string]any) string {
	list, _ := input["repositories"].([]any)
	names := []string{}
	for _, name := range list {
		names = append(names, name.(string))
	}
	return strings.Join(names, ",")
}

func TestRevokedInstallationMintsNoToken(t *testing.T) {
	s := fixture(t)
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/9/access_tokens": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"Not Found","secret":"upstream-secret"}`)
		},
	})
	_, err := s.installationToken(t.Context(), tokenKey{installation: 9, repository: "acme/app", purpose: purposeClone})
	if err == nil || !strings.Contains(err.Error(), "instalação do GitHub App foi removida") || strings.Contains(err.Error(), "upstream-secret") {
		t.Fatal(err)
	}
	// A suspension recorded by the webhook blocks even a cached token.
	s.tokens[tokenKey{installation: 10, repository: "acme/app", purpose: purposeClone}] = cachedToken{"cached", time.Now().Add(time.Hour)}
	if _, err := s.db.Exec(`INSERT INTO git_provider_installations(id,suspended) VALUES(10,1)`); err != nil {
		t.Fatal(err)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("suspended installation reached GitHub") })
	if _, err := s.installationToken(t.Context(), tokenKey{installation: 10, repository: "acme/app", purpose: purposeClone}); err == nil {
		t.Fatal("suspended installation accepted")
	}
}

func TestRevokedTokenIsDroppedOnUnauthorized(t *testing.T) {
	s := fixture(t)
	key := tokenKey{installation: 5, repository: "acme/app", purpose: purposeReport}
	s.tokens[key] = cachedToken{"revoked-early", time.Now().Add(time.Hour)}
	mock(s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	err := s.withInstallationToken(t.Context(), key, func(token string) error {
		return s.github(t.Context(), http.MethodGet, "/x", token, nil, nil)
	})
	if statusOf(err) != http.StatusUnauthorized {
		t.Fatal(err)
	}
	if _, found := s.cachedToken(key); found {
		t.Fatal("revoked token kept in cache")
	}
}

func TestCredentialsFailClosedWithoutSelectionSentinelMisuse(t *testing.T) {
	s := fixture(t)
	called := false
	err := s.WithCredentials(t.Context(), KindApplication, "app", func(string, string) error { called = true; return nil })
	if !errors.Is(err, ErrNoSelection) || called {
		t.Fatal(err, called)
	}
	if _, err := s.db.Exec(`INSERT INTO git_provider_selections(kind,resource_id,installation_id,repository)
		VALUES('application','app',123,'acme/app')`); err != nil {
		t.Fatal(err)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	err = s.WithCredentials(t.Context(), KindApplication, "app", func(string, string) error { called = true; return nil })
	if err == nil || errors.Is(err, ErrNoSelection) || called {
		t.Fatal("a failing selected installation must not look like a missing selection", err)
	}
	if err := s.ClearSelection(t.Context(), KindApplication, "app"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Selection(t.Context(), KindApplication, "app"); !errors.Is(err, ErrNoSelection) {
		t.Fatal(err)
	}
}

func TestSelectRequiresRepositoryOfInstallationAndResource(t *testing.T) {
	s := fixture(t)
	mints := 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/7/access_tokens": tokenHandler("list", time.Hour, &mints),
		"GET /installation/repositories": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"repositories": []Repository{{ID: 1, FullName: "Acme/App"}}})
		},
	})
	if err := s.Select(t.Context(), KindApplication, "app", Selection{7, "acme/other"}); err == nil {
		t.Fatal("repository outside the installation accepted")
	}
	if err := s.Select(t.Context(), KindApplication, "missing", Selection{7, "acme/app"}); err == nil {
		t.Fatal("missing resource accepted")
	}
	if err := s.Select(t.Context(), KindStack, "stack", Selection{7, "acme/app"}); err != nil {
		t.Fatal(err)
	}
	selection, err := s.Selection(t.Context(), KindStack, "stack")
	if err != nil || selection.Repository != "Acme/App" || selection.CloneURL() != "https://github.com/Acme/App.git" {
		t.Fatal(selection, err)
	}
	if _, err := s.db.Exec(`DELETE FROM stacks WHERE id='stack'`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Selection(t.Context(), KindStack, "stack"); !errors.Is(err, ErrNoSelection) {
		t.Fatal("selection survived the deleted stack", err)
	}
}
