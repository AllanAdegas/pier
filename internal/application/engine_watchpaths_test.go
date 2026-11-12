package application

import (
	"context"
	"net/http"
	"testing"
)

func TestEngineWatchPathsCoverNestedAddedModifiedAndRemovedFiles(t *testing.T) {
	cases := []struct {
		pattern, file string
		match         bool
	}{
		{"apps/api/**", "apps/api/main.go", true}, {"apps/api/**", "apps/api/src/server/main.go", true},
		{"apps/*/src/*.go", "apps/api/src/main.go", true}, {"apps/*/src/*.go", "apps/api/src/internal/main.go", false},
		{"**/package.json", "package.json", true}, {"**/package.json", "apps/web/package.json", true},
		{"apps/api/**", "apps/web/main.go", false}, {"README.?d", "README.md", true},
	}
	for _, c := range cases {
		if got := watchMatch(c.pattern, c.file); got != c.match {
			t.Errorf("%s %s: %v", c.pattern, c.file, got)
		}
	}
	for _, body := range []string{`{"commits":[{"added":["apps/api/new.go"]}]}`, `{"commits":[{"modified":["apps/api/main.go"]}]}`, `{"commits":[{"removed":["apps/api/old.go"]}]}`} {
		if !watchPathsChanged([]string{"apps/api/**"}, []byte(body)) {
			t.Fatal(body)
		}
	}
	if watchPathsChanged([]string{"apps/api/**"}, []byte(`{"commits":[{"modified":["apps/web/main.ts"]}]}`)) {
		t.Fatal("unrelated change matched")
	}
	for _, body := range []string{`{}`, `{"commits":[{}]}`, `{"total_commits_count":2,"commits":[{"modified":["unrelated"]}]}`, `{"truncated":true,"commits":[{"modified":["unrelated"]}]}`} {
		if !watchPathsChanged([]string{"apps/api/**"}, []byte(body)) {
			t.Fatal("incomplete payload skipped", body)
		}
	}
	for _, pattern := range []string{"/absolute/**", "../secret", "a/[", "a//b", "a/./b"} {
		if validWatchPattern(pattern) {
			t.Fatal(pattern)
		}
	}
}

func TestEngineGitProviderPushFilteringAndManualBypass(t *testing.T) {
	for _, provider := range []GitProvider{ProviderGitHub, ProviderGitLab, ProviderGitea} {
		t.Run(string(provider), func(t *testing.T) {
			config := gitConfig()
			config.AutoDeploy = true
			config.Source.WatchPaths = []string{"apps/api/**"}
			f := newDeployFixture(t, config)
			dispatcher := NewDispatcher(f.store, f.deployer, f.deployer.logger, 0)
			service := NewService(f.store, f.env, fakeConnector{f.ex}, dispatcher)
			secret, err := service.WebhookSecret(context.Background(), f.app.ID)
			if err != nil {
				t.Fatal(err)
			}
			send := func(body string) WebhookResult {
				header := http.Header{}
				switch provider {
				case ProviderGitHub:
					header.Set("X-GitHub-Event", "push")
					header.Set("X-Hub-Signature-256", "sha256="+sign(secret, body))
				case ProviderGitLab:
					header.Set("X-Gitlab-Event", "Push Hook")
					header.Set("X-Gitlab-Token", secret)
				case ProviderGitea:
					header.Set("X-Gitea-Event", "push")
					header.Set("X-Gitea-Signature", sign(secret, body))
				}
				result, err := service.HandleGitPush(context.Background(), f.app.ID, provider, header, []byte(body))
				if err != nil {
					t.Fatal(err)
				}
				return result
			}
			if result := send(`{"ref":"refs/heads/main","commits":[{"modified":["apps/web/index.ts"]}]}`); result.Deployed {
				t.Fatal("unrelated push deployed")
			}
			if _, err := service.Deploy(context.Background(), f.app.ID, TriggerManual); err != nil {
				t.Fatal("manual deploy filtered", err)
			}
			if result := send(`{"ref":"refs/heads/main","commits":[{"removed":["apps/api/old.go"]}]}`); !result.Deployed {
				t.Fatal("matching deletion skipped")
			}
		})
	}
}

func TestEngineKnownEmptyChangeListsDoNotDeploy(t *testing.T) {
	if watchPathsChanged([]string{"apps/api/**"}, []byte(`{"commits":[{"added":[],"modified":[],"removed":[]}]}`)) {
		t.Fatal("empty push deployed")
	}
	if !watchPathsChanged([]string{"apps/api/**"}, []byte(`{"total_commits":3,"commits":[{"modified":["other"]}]}`)) {
		t.Fatal("truncated Gitea payload skipped")
	}
}
