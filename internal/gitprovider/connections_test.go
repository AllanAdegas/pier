package gitprovider

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestConnectionLifecycle(t *testing.T) {
	for _, provider := range []string{ProviderGitLab, ProviderGitea, ProviderBitbucket} {
		t.Run(provider, func(t *testing.T) {
			s := fixture(t)
			c := NewConnections(s)
			ctx := context.Background()
			base := "https://forge.example/sub"
			if provider == ProviderBitbucket {
				base = bitbucketCloud
			}
			created, updated, deleted := 0, 0, 0
			deleteOnUpdate := false
			mock(s, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("PRIVATE-TOKEN") != "secret-token" && !strings.Contains(r.Header.Get("Authorization"), "secret-token") {
					t.Error("missing authentication")
				}
				switch {
				case strings.HasSuffix(r.URL.Path, "/user"):
					w.Write([]byte(`{"username":"alice","login":"alice"}`))
				case strings.HasSuffix(r.URL.Path, "/personal_access_tokens/self"):
					w.Write([]byte(`{"scopes":["api"]}`))
				case strings.Contains(r.URL.Path, "/hooks"):
					switch r.Method {
					case "GET":
						if provider == ProviderBitbucket {
							w.Write([]byte(`{"values":[]}`))
						} else {
							w.Write([]byte(`[]`))
						}
					case "POST":
						created++
						w.Write([]byte(`{"id":12,"uuid":"hook-id"}`))
					case "PUT", "PATCH":
						updated++
						if deleteOnUpdate {
							if _, err := s.db.Exec(`DELETE FROM applications WHERE id='app'`); err != nil {
								t.Error(err)
							}
						}
						w.Write([]byte(`{}`))
					case "DELETE":
						deleted++
						w.WriteHeader(204)
					}
				default:
					row := map[string]any{"path_with_namespace": "owner/repo", "full_name": "owner/repo", "http_url_to_repo": base + "/owner/repo.git", "clone_url": base + "/owner/repo.git", "default_branch": "main"}
					if strings.HasSuffix(r.URL.Path, "/projects") || strings.HasSuffix(r.URL.Path, "/user/repos") || r.URL.Path == "/2.0/repositories" {
						if provider == ProviderBitbucket {
							json.NewEncoder(w).Encode(map[string]any{"values": []any{row}, "next": "https://evil.example/steal"})
						} else {
							json.NewEncoder(w).Encode([]any{row})
						}
					} else {
						json.NewEncoder(w).Encode(row)
					}
				}
			})
			result, err := c.Create(ctx, ConnectionInput{Provider: provider, BaseURL: base, Token: "secret-token"})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "secret-token") {
				t.Fatal("token leaked")
			}
			var sealed string
			if err := s.db.QueryRow(`SELECT encrypted_token FROM git_connections WHERE id=?`, result.Connection.ID).Scan(&sealed); err != nil || sealed == "secret-token" {
				t.Fatal("token not sealed", err)
			}
			page, err := c.Repositories(ctx, result.Connection.ID, "", 1)
			if err != nil || len(page.Items) != 1 {
				t.Fatalf("repositories: %+v %v", page, err)
			}
			if _, err := s.db.Exec(`UPDATE applications SET config=json_object('source',json_object('repository',?)) WHERE id='app'`, base+"/owner/repo.git"); err != nil {
				t.Fatal(err)
			}
			req := LinkRequest{Kind: KindApplication, ResourceID: "app", ConnectionID: result.Connection.ID, Repository: "owner/repo", Source: base + "/owner/repo.git", Hook: HookTarget{URL: "https://pier.example/webhooks/applications/app/" + provider, Secret: webhookSecret}}
			bad := req
			bad.Source = "https://evil.example/owner/repo.git"
			if _, err := c.Connect(ctx, bad); err == nil {
				t.Fatal("accepted source outside selected repository")
			}
			if created != 0 || updated != 0 {
				t.Fatal("source rejection mutated remote hooks")
			}
			for i := 0; i < 2; i++ {
				link, err := c.Connect(ctx, req)
				if err != nil || link.Webhook != WebhookActive {
					t.Fatalf("link: %+v %v", link, err)
				}
			}
			if created != 1 || updated != 1 {
				t.Fatalf("duplicate webhook: %d %d", created, updated)
			}
			if err := c.WithCredentials(ctx, KindApplication, "app", func(repository, token string) error {
				if token != "secret-token" || !strings.Contains(repository, "owner/repo.git") {
					t.Error("incorrect clone credentials")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := s.db.Exec(`UPDATE applications SET config=json_object('source',json_object('repository','https://forge.example/foreign.git')) WHERE id='app'`); err != nil {
				t.Fatal(err)
			}
			called := false
			if err := c.WithCredentials(ctx, KindApplication, "app", func(string, string) error { called = true; return nil }); err == nil || called {
				t.Fatal("mismatched source resolved credentials")
			}
			deleteOnUpdate = true
			if _, err := c.RefreshWebhook(ctx, KindApplication, "app", req.Hook); err != nil {
				t.Fatal(err)
			}
			stored, found, err := c.linkRecord(ctx, KindApplication, "app")
			if err != nil || !found || !stored.orphaned {
				t.Fatal("remote mutation resurrected deleted resource", stored, err)
			}
			if err := c.Delete(ctx, result.Connection.ID); err == nil {
				t.Fatal("deleted linked connection")
			}
			if err := c.Disconnect(ctx, KindApplication, "app"); err != nil {
				t.Fatal(err)
			}
			if deleted != 1 {
				t.Fatal("webhook not removed")
			}
			if err := c.Delete(ctx, result.Connection.ID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestConnectionBoundaries(t *testing.T) {
	for _, address := range []string{"http://forge.example", "https://user:secret@forge.example", "https://forge.example?token=x", "https://forge.example/../api"} {
		if _, err := normalizeBaseURL(ProviderGitea, address); err == nil {
			t.Errorf("accepted %s", address)
		}
	}
	for _, address := range []string{"https://evil.example/root/repo.git", "https://forge.example/root/../repo.git", "https://forge.example/rooted/repo.git"} {
		if withinBase(address, "https://forge.example/root") {
			t.Errorf("escaped base: %s", address)
		}
	}
	for _, status := range []int{401, 403, 500} {
		_, state, code := hookOutcome("", &upstreamError{provider: "test", status: status})
		want := WebhookManual
		if status == 500 {
			want = WebhookFailed
		}
		if state != want || code != status {
			t.Fatal(state, code)
		}
	}
	if sameRepositoryURL("https://forge.example/Owner/Repo.git", "https://forge.example/owner/repo.git") {
		t.Fatal("case-sensitive path changed")
	}
}

func TestConnectionRejectsHostileRepositoryAndRedactsErrors(t *testing.T) {
	s := fixture(t)
	c := NewConnections(s)
	ctx := context.Background()
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/user"):
			w.Write([]byte(`{"login":"alice"}`))
		case strings.HasSuffix(r.URL.Path, "/user/repos"):
			w.Write([]byte(`[{"full_name":"owner/repo","clone_url":"https://evil.example/owner/repo.git"}]`))
		default:
			w.WriteHeader(403)
			w.Write([]byte(`secret-token`))
		}
	})
	result, err := c.Create(ctx, ConnectionInput{Provider: ProviderGitea, BaseURL: "https://forge.example", Token: "secret-token"})
	if err != nil {
		t.Fatal(err)
	}
	page, err := c.Repositories(ctx, result.Connection.ID, "", 1)
	if err != nil || len(page.Items) != 0 {
		t.Fatal("hostile clone address accepted", page, err)
	}
	_, err = c.Branches(ctx, result.Connection.ID, "owner/repo", "", 1)
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal("provider error not sanitized", err)
	}
	_, err = c.Branches(ctx, result.Connection.ID, "../repo", "", 1)
	if err == nil {
		t.Fatal("path traversal accepted")
	}
}

func TestConnectionDeletedResourceCleanupRetries(t *testing.T) {
	s := fixture(t)
	c := NewConnections(s)
	ctx := context.Background()
	status := 500
	calls := 0
	sealed, err := s.box.Seal("secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.insert(ctx, connectionRecord{Connection: Connection{ID: "connection", Provider: ProviderGitea, Name: "forge", BaseURL: "https://forge.example", Scopes: []string{}}, sealedToken: sealed}); err != nil {
		t.Fatal(err)
	}
	if err := c.saveLink(ctx, linkRecord{kind: KindApplication, resourceID: "app", connectionID: "connection", repository: "owner/repo", cloneURL: "https://forge.example/owner/repo.git", webhookID: "12", webhookStatus: WebhookActive}); err != nil {
		t.Fatal(err)
	}
	mock(s, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "DELETE" {
			t.Error(r.Method)
		}
		w.WriteHeader(status)
	})
	if _, err := s.db.Exec(`DELETE FROM applications WHERE id='app'`); err != nil {
		t.Fatal(err)
	}
	if n, err := c.Sweep(ctx); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if err := c.Delete(ctx, "connection"); err == nil {
		t.Fatal("deleted credentials needed for pending cleanup")
	}
	for _, denied := range []int{401, 403} {
		status = denied
		if n, err := c.Sweep(ctx); err != nil || n != 1 {
			t.Fatalf("authorization failure discarded cleanup: %d %v", n, err)
		}
		if err := c.Delete(ctx, "connection"); err == nil {
			t.Fatal("deleted credentials after denied webhook cleanup")
		}
	}
	status = 204
	if n, err := c.Sweep(ctx); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if calls != 4 {
		t.Fatal(calls)
	}
	if err := c.Delete(ctx, "connection"); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionHookPaginationFailsClosed(t *testing.T) {
	for _, provider := range []string{ProviderGitLab, ProviderGitea, ProviderBitbucket} {
		t.Run(provider, func(t *testing.T) {
			s := fixture(t)
			c := NewConnections(s)
			pages, mutations := 0, 0
			mock(s, func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "/hooks") {
					if r.Method != "GET" {
						mutations++
						t.Error("unexpected mutation")
						return
					}
					pages++
					rows := make([]map[string]any, hookPageSize)
					for i := range rows {
						rows[i] = map[string]any{"id": i + 1, "url": "https://other.example", "config": map[string]string{"url": "https://other.example"}}
					}
					if provider == ProviderBitbucket {
						json.NewEncoder(w).Encode(map[string]any{"values": rows, "next": "more"})
					} else {
						json.NewEncoder(w).Encode(rows)
					}
					return
				}
				w.Write([]byte(`{"username":"alice","login":"alice"}`))
			})
			base := "https://forge.example"
			if provider == ProviderBitbucket {
				base = bitbucketCloud
			}
			created, err := c.Create(t.Context(), ConnectionInput{Provider: provider, BaseURL: base, Token: "secret"})
			if err != nil {
				t.Fatal(err)
			}
			_, client, err := c.open(t.Context(), created.Connection.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, state, _ := syncHook(t.Context(), client, "owner/repo", "", hookSpec{HookTarget: HookTarget{URL: "https://pier.example/hook"}})
			if state != WebhookFailed || pages != maxHookPages || mutations != 0 {
				t.Fatal(state, pages, mutations)
			}
		})
	}
}

func TestConnectionSaveAfterDeletionRemainsOrphaned(t *testing.T) {
	s := fixture(t)
	c := NewConnections(s)
	s.db.Exec(`PRAGMA foreign_keys=OFF`)
	if _, err := s.db.Exec(`DELETE FROM applications WHERE id='app'`); err != nil {
		t.Fatal(err)
	}
	if err := c.saveLink(t.Context(), linkRecord{kind: KindApplication, resourceID: "app", connectionID: "missing", repository: "owner/repo", webhookID: "created-remotely", webhookStatus: WebhookActive}); err != nil {
		t.Fatal(err)
	}
	link, found, err := c.linkRecord(t.Context(), KindApplication, "app")
	if err != nil || !found || !link.orphaned {
		t.Fatal(link, found, err)
	}
}
