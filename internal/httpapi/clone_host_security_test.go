package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rbac "pier/internal/access"
	"pier/internal/application"
	"pier/internal/envvar"
)

func TestCloneHostOptionsRequireFreshInstanceOwnerAuthorization(t *testing.T) {
	for _, role := range []rbac.Role{rbac.Admin, rbac.Owner} {
		t.Run(string(role), func(t *testing.T) {
			w := newAccessWorld(t)
			env := envvar.NewStore(w.a.Instance.DB, w.a.Instance.Box)
			w.a.Env = env
			w.a.Applications = application.NewService(application.NewStore(w.a.Instance.DB, w.a.Instance.Box), env, nil, nil)
			w.handler = w.a.Handler()
			user, token := testIdentity(t, w.a, "clone-project-"+string(role))
			grantMember(t, w.a, user.ID, "pa", role)
			grantMember(t, w.a, user.ID, "pb", role)
			cookie, err := w.a.Identity.OpenSession(t.Context(), user.ID)
			if err != nil {
				t.Fatal(err)
			}
			owner, ownerToken := testIdentity(t, w.a, "clone-instance-owner")
			grantRole(t, w.a, owner.ID, rbac.Owner)
			call := func(credential, env string, session bool) *httptest.ResponseRecorder {
				r := httptest.NewRequest(http.MethodPost, "/api/v1/applications/appa/clone", strings.NewReader(fmt.Sprintf(`{"target_environment_id":%q,"name":"copy"}`, env)))
				if session {
					r.AddCookie(&http.Cookie{Name: sessionCookie, Value: credential})
					r.Header.Set(clientHeader, "web")
				} else {
					r.Header.Set("Authorization", "Bearer "+credential)
				}
				r.Header.Set("Content-Type", "application/json")
				rec := httptest.NewRecorder()
				w.handler.ServeHTTP(rec, r)
				return rec
			}
			count := func() int {
				var n int
				if err := w.a.Instance.DB.QueryRow("SELECT count(*) FROM applications").Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			writeConfig := func(config application.Config) {
				raw, _ := json.Marshal(config)
				if _, err := w.a.Instance.DB.Exec("UPDATE applications SET config=? WHERE id='appa'", string(raw)); err != nil {
					t.Fatal(err)
				}
			}
			for _, enabled := range []bool{false, true} {
				if _, err := w.a.Applications.SetDockerOptionsPolicy(t.Context(), application.DockerOptionsPolicy{AllowDangerous: enabled, AllowDevices: enabled}); err != nil {
					t.Fatal(err)
				}
				for _, kind := range []string{"bind", "privileged", "devices", "capability"} {
					cfg := application.DefaultConfig(application.PackImage)
					cfg.Source.Image = "nginx"
					switch kind {
					case "bind":
						cfg.Storages = []application.Storage{{Kind: application.StorageBind, Source: "/tmp/clone-host", Target: "/host"}}
					case "privileged":
						cfg.DockerOptions.Privileged = true
					case "devices":
						cfg.DockerOptions.Devices = []string{"/dev/alias"}
					case "capability":
						cfg.DockerOptions.CapAdd = []string{"SYS_ADMIN"}
					}
					writeConfig(cfg)
					for _, env := range []string{"ea", "eb"} {
						for _, session := range []bool{false, true} {
							credential := token
							if session {
								credential = cookie
							}
							before := count()
							rec := call(credential, env, session)
							if rec.Code != 403 || count() != before {
								t.Fatalf("role=%s enabled=%v kind=%s env=%s session=%v: %d %s", role, enabled, kind, env, session, rec.Code, rec.Body.String())
							}
						}
					}
					before := count()
					rec := call(ownerToken, "ea", false)
					if enabled {
						if rec.Code != 201 || count() != before+1 {
							t.Fatalf("authorized owner: %d %s", rec.Code, rec.Body.String())
						}
					} else if rec.Code == 201 || count() != before {
						t.Fatalf("owner bypassed disabled policy: %d", rec.Code)
					}
				}
			}
			normal := application.DefaultConfig(application.PackImage)
			normal.Source.Image = "nginx"
			normal.Storages = []application.Storage{{Kind: application.StorageVolume, Source: "data", Target: "/data"}}
			writeConfig(normal)
			if rec := call(token, "ea", false); rec.Code != 201 {
				t.Fatalf("named volume clone: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
