package gitprovider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

var sha = strings.Repeat("a", 40)

const providerToken = "provider-secret-token"

func TestFeedbackDefaultsFollowTheGitHubAppLink(t *testing.T) {
	s := fixture(t)
	settings, err := s.FeedbackSettings(t.Context(), KindApplication, "app")
	if err != nil || settings.Provider != ProviderNone || settings.CommitStatus {
		t.Fatal(settings, err)
	}
	if _, err := s.Feedback(t.Context(), KindApplication, "app", "https://github.com/acme/app.git"); !errors.Is(err, ErrNoFeedback) {
		t.Fatal(err)
	}
	s.db.Exec(`INSERT INTO git_provider_selections VALUES('application','app',123,'acme/app')`)
	settings, _ = s.FeedbackSettings(t.Context(), KindApplication, "app")
	if settings.Provider != ProviderGitHub || !settings.CommitStatus || !settings.PullRequestComments || settings.Saved {
		t.Fatal("GitHub App link must default feedback on", settings)
	}
	saved, err := s.SaveFeedback(t.Context(), KindApplication, "app", "https://github.com/acme/app.git",
		FeedbackSettings{Provider: ProviderGitHub, CommitStatus: true})
	if err != nil || !saved.Saved || saved.PullRequestComments {
		t.Fatal(saved, err)
	}
	feedback, err := s.Feedback(t.Context(), KindApplication, "app", "https://github.com/acme/app.git")
	if err != nil || !feedback.CommitStatus || feedback.PullRequestComments {
		t.Fatal(feedback, err)
	}
	if _, err := s.SaveFeedback(t.Context(), KindStack, "stack", "", FeedbackSettings{Provider: ProviderGitHub}); err == nil {
		t.Fatal("GitHub feedback without a link accepted")
	}
}

func TestFeedbackTokenIsSealedWriteOnlyAndBoundToItsOrigin(t *testing.T) {
	s := fixture(t)
	repository := "https://gitlab.example.com/group/app.git"
	saved, err := s.SaveFeedback(t.Context(), KindApplication, "app", repository,
		FeedbackSettings{Provider: ProviderGitLab, CommitStatus: true, Token: providerToken})
	if err != nil || !saved.TokenSet || saved.Token != "" {
		t.Fatal(saved, err)
	}
	encoded, _ := json.Marshal(saved)
	if strings.Contains(string(encoded), providerToken) {
		t.Fatal("token returned")
	}
	var sealed string
	s.db.QueryRow(`SELECT encrypted_token FROM git_provider_feedback`).Scan(&sealed)
	if sealed == "" || strings.Contains(sealed, providerToken) {
		t.Fatal("token not sealed")
	}
	// Saving again without a token keeps it; changing provider or host needs a new one.
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", repository,
		FeedbackSettings{Provider: ProviderGitLab, PullRequestComments: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", repository, FeedbackSettings{Provider: ProviderGitea}); err == nil {
		t.Fatal("token carried to another provider")
	}
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", "https://evil.example/group/app.git",
		FeedbackSettings{Provider: ProviderGitLab}); err == nil {
		t.Fatal("token carried to another host")
	}
	// A source moved to another host never receives the token.
	mock(s, func(w http.ResponseWriter, r *http.Request) { t.Fatal("token sent to the new host", r.URL) })
	if _, err := s.Feedback(t.Context(), KindApplication, "app", "https://evil.example/group/app.git"); !errors.Is(err, ErrNoFeedback) {
		t.Fatal(err)
	}
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", "https://gitlab.com/a/b.git",
		FeedbackSettings{Provider: ProviderBitbucket, Token: "x"}); err == nil {
		t.Fatal("Bitbucket feedback for a non-Bitbucket repository accepted")
	}
}

func TestGitHubReporterUpdatesItsOwnCommentInPlace(t *testing.T) {
	s := fixture(t)
	mints, statuses, patches, posts := 0, 0, 0, 0
	githubFake(t, s, map[string]http.HandlerFunc{
		"POST /app/installations/123/access_tokens": tokenHandler("report-token", time.Hour, &mints),
		"POST /repos/acme/app/statuses/" + sha: func(w http.ResponseWriter, r *http.Request) {
			statuses++
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			if input["state"] != "pending" || input["context"] != "pier/deploy" || r.Header.Get("Authorization") != "Bearer report-token" {
				t.Error(input)
			}
			w.WriteHeader(http.StatusCreated)
		},
		"GET /repos/acme/app/issues/3/comments": func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, `[{"id":4,"body":"<!-- pier:preview:app -->\nforged","performed_via_github_app":null},
				{"id":5,"body":"<!-- pier:preview:app -->\nold","performed_via_github_app":{"id":42}}]`)
		},
		"PATCH /repos/acme/app/issues/comments/5": func(w http.ResponseWriter, r *http.Request) { patches++ },
		"POST /repos/acme/app/issues/3/comments":  func(w http.ResponseWriter, r *http.Request) { posts++ },
	})
	reporter := &githubReporter{service: s, selection: Selection{123, "acme/app"}}
	if err := reporter.SetCommitStatus(t.Context(), CommitStatus{SHA: sha, State: StatePending, Context: "pier/deploy"}); err != nil {
		t.Fatal(err)
	}
	if err := reporter.UpsertComment(t.Context(), Comment{Number: 3, Marker: "pier:preview:app", Body: "ready"}); err != nil {
		t.Fatal(err)
	}
	if statuses != 1 || patches != 1 || posts != 0 || mints != 1 {
		t.Fatal(statuses, patches, posts, mints)
	}
	if err := reporter.SetCommitStatus(t.Context(), CommitStatus{SHA: "HEAD", State: StateSuccess, Context: "x"}); err == nil {
		t.Fatal("invalid SHA sent")
	}
}

// providerFake is an HTTPS fake of a self-hosted provider API that keeps
// one pull request thread, so tests can check comments are not duplicated.
type providerFake struct {
	t        *testing.T
	mu       sync.Mutex
	comments map[int64]string
	authors  map[int64]int64
	next     int64
	statuses []string
	auth     string
}

func newProviderFake(t *testing.T, auth string) *providerFake {
	return &providerFake{t: t, comments: map[int64]string{}, authors: map[int64]int64{}, next: 1, auth: auth}
}

func (f *providerFake) authorized(r *http.Request) bool {
	if r.Header.Get("PRIVATE-TOKEN")+r.Header.Get("Authorization") != f.auth {
		f.t.Errorf("wrong credentials on %s", r.URL.Path)
		return false
	}
	return true
}

func (f *providerFake) list(w http.ResponseWriter, author string) {
	rows := []map[string]any{}
	for id, body := range f.comments {
		rows = append(rows, map[string]any{"id": id, "body": body, author: map[string]int64{"id": f.authors[id]}})
	}
	json.NewEncoder(w).Encode(rows)
}

func (f *providerFake) save(r *http.Request, id int64, author int64) {
	var input map[string]string
	json.NewDecoder(r.Body).Decode(&input)
	if id == 0 {
		id = f.next
		f.next++
	}
	f.comments[id], f.authors[id] = input["body"], author
}

func (f *providerFake) status(r *http.Request, field string) {
	var input map[string]string
	json.NewDecoder(r.Body).Decode(&input)
	f.statuses = append(f.statuses, input[field]+"@"+input["name"]+input["context"])
}

func TestGitLabReporterUsesTheProjectAPIAndUpdatesTheNote(t *testing.T) {
	fake := newProviderFake(t, providerToken)
	fake.comments[90], fake.authors[90] = "<!-- pier:preview:app -->\nforged by someone else", 666
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if !fake.authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		path := r.URL.EscapedPath()
		switch {
		case r.Method == "GET" && path == "/api/v4/user":
			io.WriteString(w, `{"id":7}`)
		case r.Method == "POST" && path == "/api/v4/projects/group%2Fsub%2Fapp/statuses/"+sha:
			fake.status(r, "state")
		case r.Method == "GET" && path == "/api/v4/projects/group%2Fsub%2Fapp/merge_requests/4/notes":
			fake.list(w, "author")
		case r.Method == "POST" && path == "/api/v4/projects/group%2Fsub%2Fapp/merge_requests/4/notes":
			fake.save(r, 0, 7)
		case r.Method == "PUT" && strings.HasPrefix(path, "/api/v4/projects/group%2Fsub%2Fapp/merge_requests/4/notes/"):
			fake.save(r, 1, 7)
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
		}
	}))
	defer server.Close()
	s := fixtureWithClient(t, server.Client())
	repository := server.URL + "/group/sub/app.git"
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", repository,
		FeedbackSettings{Provider: ProviderGitLab, CommitStatus: true, PullRequestComments: true, Token: providerToken}); err != nil {
		t.Fatal(err)
	}
	feedback, err := s.Feedback(t.Context(), KindApplication, "app", repository)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []State{StatePending, StateSuccess, StateFailure, StateError} {
		if err := feedback.Reporter.SetCommitStatus(t.Context(), CommitStatus{SHA: sha, State: state, Context: "pier/app"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, body := range []string{"building", "ready", "ready"} {
		if err := feedback.Reporter.UpsertComment(t.Context(), Comment{Number: 4, Marker: "pier:preview:app", Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	if strings.Join(fake.statuses, ",") != "running@pier/app,success@pier/app,failed@pier/app,canceled@pier/app" {
		t.Fatal(fake.statuses)
	}
	if len(fake.comments) != 2 || fake.comments[1] != "<!-- pier:preview:app -->\nready" || !strings.Contains(fake.comments[90], "forged") {
		t.Fatal(fake.comments)
	}
}

func TestGiteaReporterUsesTokenAuthAndUpdatesTheComment(t *testing.T) {
	fake := newProviderFake(t, "token "+providerToken)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		if !fake.authorized(r) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch path := r.URL.Path; {
		case r.Method == "GET" && path == "/api/v1/user":
			io.WriteString(w, `{"id":3}`)
		case r.Method == "POST" && path == "/api/v1/repos/owner/app/statuses/"+sha:
			fake.status(r, "state")
		case r.Method == "GET" && path == "/api/v1/repos/owner/app/issues/8/comments":
			fake.list(w, "user")
		case r.Method == "POST" && path == "/api/v1/repos/owner/app/issues/8/comments":
			fake.save(r, 0, 3)
		case r.Method == "PATCH" && path == "/api/v1/repos/owner/app/issues/comments/1":
			fake.save(r, 1, 3)
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
		}
	}))
	defer server.Close()
	s := fixtureWithClient(t, server.Client())
	// An SSH remote of the same host maps to the HTTPS API origin.
	repository := server.URL + "/owner/app.git"
	if _, err := s.SaveFeedback(t.Context(), KindStack, "stack", repository,
		FeedbackSettings{Provider: ProviderGitea, CommitStatus: true, PullRequestComments: true, Token: providerToken}); err != nil {
		t.Fatal(err)
	}
	feedback, err := s.Feedback(t.Context(), KindStack, "stack", repository)
	if err != nil {
		t.Fatal(err)
	}
	feedback.Reporter.SetCommitStatus(t.Context(), CommitStatus{SHA: sha, State: StateSuccess, Context: "pier/app"})
	feedback.Reporter.UpsertComment(t.Context(), Comment{Number: 8, Marker: "pier:preview:app", Body: "one"})
	feedback.Reporter.UpsertComment(t.Context(), Comment{Number: 8, Marker: "pier:preview:app", Body: "two"})
	if strings.Join(fake.statuses, ",") != "success@pier/app" || len(fake.comments) != 1 || !strings.HasSuffix(fake.comments[1], "\ntwo") {
		t.Fatal(fake.statuses, fake.comments)
	}
}

func TestBitbucketReporterAndCommitResolution(t *testing.T) {
	s := fixture(t)
	var statuses []map[string]string
	comments := map[string]string{}
	auth := "Bearer " + providerToken
	check := func(r *http.Request) {
		if r.Header.Get("Authorization") != auth {
			t.Errorf("wrong credentials on %s", r.URL.Path)
		}
	}
	githubFake(t, s, map[string]http.HandlerFunc{
		"GET /2.0/user": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) },
		"POST /2.0/repositories/ws/app/commit/" + sha + "/statuses/build": func(w http.ResponseWriter, r *http.Request) {
			check(r)
			var input map[string]string
			json.NewDecoder(r.Body).Decode(&input)
			statuses = append(statuses, input)
		},
		"GET /2.0/repositories/ws/app/pullrequests/2/comments": func(w http.ResponseWriter, r *http.Request) {
			check(r)
			values := []map[string]any{}
			for id, body := range comments {
				values = append(values, map[string]any{"id": len(id), "content": map[string]string{"raw": body}})
			}
			json.NewEncoder(w).Encode(map[string]any{"values": values})
		},
		"POST /2.0/repositories/ws/app/pullrequests/2/comments": func(w http.ResponseWriter, r *http.Request) {
			var input struct{ Content struct{ Raw string } }
			json.NewDecoder(r.Body).Decode(&input)
			comments["x"] = input.Content.Raw
		},
		"PUT /2.0/repositories/ws/app/pullrequests/2/comments/1": func(w http.ResponseWriter, r *http.Request) {
			var input struct{ Content struct{ Raw string } }
			json.NewDecoder(r.Body).Decode(&input)
			comments["x"] = input.Content.Raw
		},
		"GET /2.0/repositories/ws/app/commit/abcdef123456": func(w http.ResponseWriter, r *http.Request) {
			check(r)
			io.WriteString(w, `{"hash":"abcdef1234567890abcdef1234567890abcdef12"}`)
		},
	})
	repository := "git@bitbucket.org:ws/app.git"
	if _, err := s.SaveFeedback(t.Context(), KindApplication, "app", repository,
		FeedbackSettings{Provider: ProviderBitbucket, CommitStatus: true, PullRequestComments: true, Token: providerToken}); err != nil {
		t.Fatal(err)
	}
	feedback, err := s.Feedback(t.Context(), KindApplication, "app", repository)
	if err != nil {
		t.Fatal(err)
	}
	feedback.Reporter.SetCommitStatus(t.Context(), CommitStatus{SHA: sha, State: StatePending, Context: "pier/app"})
	feedback.Reporter.UpsertComment(t.Context(), Comment{Number: 2, Marker: "pier:preview:app", Body: "one"})
	feedback.Reporter.UpsertComment(t.Context(), Comment{Number: 2, Marker: "pier:preview:app", Body: "two"})
	if len(statuses) != 1 || statuses[0]["state"] != "INPROGRESS" || statuses[0]["url"] != "https://bitbucket.org/ws/app" {
		t.Fatal(statuses)
	}
	if len(comments) != 1 || !strings.HasSuffix(comments["x"], "\ntwo") {
		t.Fatal(comments)
	}
	full, err := s.ResolveBitbucketCommit(t.Context(), KindApplication, "app", "ws/app", "ABCDEF123456")
	if err != nil || full != "abcdef1234567890abcdef1234567890abcdef12" {
		t.Fatal(full, err)
	}
	if _, err := s.ResolveBitbucketCommit(t.Context(), KindApplication, "app", "ws/app", "not-a-sha"); err == nil {
		t.Fatal("invalid commit accepted")
	}
}

func TestRemoteParsing(t *testing.T) {
	for input, want := range map[string]string{
		"https://user:secret@gitlab.example.com/group/sub/app.git": "https://gitlab.example.com|group/sub/app",
		"http://gitea.lan:3000/owner/app":                          "http://gitea.lan:3000|owner/app",
		"git@bitbucket.org:ws/app.git":                             "https://bitbucket.org|ws/app",
		"ssh://git@gitlab.example.com:2222/group/app.git":          "https://gitlab.example.com|group/app",
	} {
		location, err := parseRemote(input)
		if err != nil || location.origin+"|"+location.path != want {
			t.Fatal(input, location, err)
		}
	}
	for _, input := range []string{"file:///etc/passwd", "https://host/a/../b", "https://host/a?x=1", "ext::sh -c id"} {
		if _, err := parseRemote(input); err == nil {
			t.Fatal("accepted", input)
		}
	}
}
