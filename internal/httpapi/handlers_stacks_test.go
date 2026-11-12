package httpapi

import (
	"errors"
	"net/http/httptest"
	"testing"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
	"pier/internal/stack"
)

func TestStackSourceRequiresSensitivePermissionBeforeServerAccess(t *testing.T) {
	api := &API{}
	for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityDeploy} {
		err := api.stackSource(httptest.NewRecorder(), requestAs(ability))
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.KindForbidden {
			t.Fatalf("ability=%s err=%v", ability, err)
		}
	}
}

func TestStackComposeIsHiddenFromReadAndDeployCallers(t *testing.T) {
	for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityDeploy} {
		item := forCaller(requestAs(ability), stack.Stack{Compose: "pier.proxy: persisted-token"})
		if item.Compose != "" {
			t.Fatalf("ability=%s exposed compose", ability)
		}
	}
}
