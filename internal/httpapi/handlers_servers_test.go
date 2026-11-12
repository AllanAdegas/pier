package httpapi

import (
	"errors"
	"net/http/httptest"
	"testing"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
)

func TestProxyLogsRequireSensitivePermissionBeforeServerAccess(t *testing.T) {
	api := &API{}
	for _, ability := range []identity.Ability{identity.AbilityRead, identity.AbilityDeploy} {
		err := api.proxyLogs(httptest.NewRecorder(), requestAs(ability))
		var appErr *apperr.Error
		if !errors.As(err, &appErr) || appErr.Kind != apperr.KindForbidden {
			t.Fatalf("ability=%s err=%v", ability, err)
		}
	}
}
