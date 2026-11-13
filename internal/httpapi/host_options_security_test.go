package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	rbac "pier/internal/access"
	"pier/internal/application"
)

func TestHostBindFullAPIRequiresInstanceOwnerAndPolicy(t *testing.T) {
	world := newAccessWorld(t)
	world.handler = world.a.Handler()
	user, token := testIdentity(t, world.a, "host-project-admin")
	grantMember(t, world.a, user.ID, "pa", rbac.Admin)
	owner, ownerToken := testIdentity(t, world.a, "host-instance-owner")
	grantRole(t, world.a, owner.ID, rbac.Owner)
	config := application.DefaultConfig(application.PackImage)
	config.Source.Image = "nginx"
	call := func(token, id string, c application.Config) *httptest.ResponseRecorder {
		body, _ := json.Marshal(application.UpdateInput{Name: "a", Config: c})
		r := httptest.NewRequest(http.MethodPatch, "/api/v1/applications/"+id, strings.NewReader(string(body)))
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		world.handler.ServeHTTP(rec, r)
		return rec
	}
	if rec := call(token, "appa", config); rec.Code != 200 {
		t.Fatalf("normal: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call(token, "appb", config); rec.Code != 403 {
		t.Fatalf("cross project: %d", rec.Code)
	}
	for _, enabled := range []bool{false, true} {
		_, err := world.a.Applications.SetDockerOptionsPolicy(t.Context(), application.DockerOptionsPolicy{AllowDangerous: enabled, AllowDevices: enabled})
		if err != nil {
			t.Fatal(err)
		}
		for _, source := range []string{"/etc", "/proc", "/data/pier", "/tmp/symlink", "../host", "/run/docker.sock"} {
			dangerous := config
			dangerous.Storages = []application.Storage{{Kind: application.StorageBind, Source: source, Target: "/host"}}
			if rec := call(token, "appa", dangerous); rec.Code != 403 {
				t.Fatalf("admin enabled=%v source=%s: %d %s", enabled, source, rec.Code, rec.Body.String())
			}
			if rec := call(token, "appb", dangerous); rec.Code != 403 {
				t.Fatalf("cross project: %d", rec.Code)
			}
		}
		for _, options := range []application.DockerOptions{
			{Privileged: true}, {PidMode: "host"}, {NetworkMode: "host"},
			{Devices: []string{"/dev/alias"}}, {CapAdd: []string{"cap_sys_admin"}},
		} {
			dangerous := config
			dangerous.DockerOptions = options
			if rec := call(token, "appa", dangerous); rec.Code != 403 {
				t.Fatalf("host options: %d %s", rec.Code, rec.Body.String())
			}
		}
		dangerous := config
		dangerous.Storages = []application.Storage{{Kind: application.StorageBind, Source: "/tmp/host", Target: "/host"}}
		rec := call(ownerToken, "appa", dangerous)
		if enabled && rec.Code != 200 {
			t.Fatalf("owner enabled: %d %s", rec.Code, rec.Body.String())
		}
		if !enabled && rec.Code == 200 {
			t.Fatal("owner bypassed policy")
		}
	}
}
