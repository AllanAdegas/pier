package httpapi

import (
	"net/http"
	"pier/internal/application"
)

// Host access is an instance-owner decision even when the instance policy
// permits it. Project administrators cannot grant host access to themselves.
func (a *API) authorizeHostOptions(r *http.Request, config application.Config) error {
	if len(config.DangerousDockerOptions()) == 0 {
		return nil
	}
	return a.requireInstanceOwner(r)
}
