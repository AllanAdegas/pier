package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"testing"
)

type accessProbe struct {
	calls                  int
	user, project, ability string
}

func (p *accessProbe) Check(_ context.Context, u, scope, a string) error {
	p.calls++
	p.user = u
	p.project = scope
	p.ability = a
	if scope != "one" {
		return apperr.Forbidden("Acesso não autorizado.")
	}
	return nil
}
func TestProjectAccessHook(t *testing.T) {
	for _, test := range []struct {
		name, scope string
		abilities   []identity.Ability
		want        int
	}{
		{"member", "one", []identity.Ability{identity.AbilityRead}, 204},
		{"other project", "two", []identity.Ability{identity.AbilityRead}, 403},
		{"unknown scope", "", []identity.Ability{identity.AbilityWrite}, 403},
		{"token cannot escalate", "one", []identity.Ability{}, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &accessProbe{}
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
			p := identity.Principal{User: identity.User{ID: "alice"}, Abilities: test.abilities}
			scope := func(context.Context, *http.Request) (string, error) { return test.scope, nil }
			w := httptest.NewRecorder()
			WithProjectAccess(probe, scope, identity.AbilityRead, p, next).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			if w.Code != test.want {
				t.Fatalf("got %d", w.Code)
			}
			if len(test.abilities) == 0 && probe.calls != 0 {
				t.Fatal("credential restriction bypassed")
			}
		})
	}
}
