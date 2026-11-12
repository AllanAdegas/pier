package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"pier/internal/identity"
	"pier/internal/platform/apperr"
)

// authorizeRequest is shared by REST and the in-process MCP adapter. Global
// collections and unknown routes are intentionally instance-owner only.
func (a *API) authorizeRequest(r *http.Request, p identity.Principal, ability identity.Ability) error {
	if a.Access == nil {
		return apperr.Forbidden("Acesso não autorizado.")
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	if path == "me" || strings.HasPrefix(path, "me/") || path == "access/accept" || path == "terminal" {
		return nil
	}
	if path == "tokens" || strings.HasPrefix(path, "tokens/") {
		if id := r.PathValue("id"); id != "" {
			return a.Access.OwnToken(r.Context(), p.User.ID, id)
		}
		return nil
	}
	parts := strings.Split(path, "/")
	project := ""
	if len(parts) > 1 && r.PathValue("id") != "" {
		var err error
		switch parts[0] {
		case "projects", "environments", "applications", "databases", "stacks", "deployments", "tasks", "backups":
			project, err = a.Access.Project(r.Context(), parts[0], r.PathValue("id"))
			if err != nil {
				return apperr.Forbidden("Acesso não autorizado.")
			}
		}
	}
	if err := p.AuthorizeAccess(r.Context(), a.Access, project, ability); err != nil {
		return err
	}
	// Body references may choose a second resource or privileged infrastructure.
	// Check every recognized ID recursively; unrecognized IDs require owner.
	if r.Body != nil && r.Body != http.NoBody && !strings.Contains(r.Header.Get("Content-Type"), "application/octet-stream") {
		raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
		if err != nil {
			return err
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var body any
		if json.Unmarshal(raw, &body) == nil {
			return a.checkReferences(r, p, ability, body)
		}
		if len(raw) > 0 {
			return p.AuthorizeAccess(r.Context(), a.Access, "", ability)
		}
	}
	return nil
}

func (a *API) checkReferences(r *http.Request, p identity.Principal, ability identity.Ability, v any) error {
	switch value := v.(type) {
	case []any:
		for _, item := range value {
			if err := a.checkReferences(r, p, ability, item); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, item := range value {
			if strings.HasSuffix(key, "_ids") {
				if err := p.AuthorizeAccess(r.Context(), a.Access, "", ability); err != nil {
					return err
				}
			}
			if id, ok := item.(string); ok && id != "" && (strings.HasSuffix(key, "_id") || strings.HasSuffix(key, "_ids")) {
				kind := map[string]string{"project_id": "projects", "environment_id": "environments", "application_id": "applications", "database_id": "databases", "stack_id": "stacks", "deployment_id": "deployments", "task_id": "tasks", "backup_id": "backups", "target_environment_id": "environments", "target_application_id": "applications", "source_application_id": "applications"}[key]
				scope := ""
				if kind != "" {
					var err error
					scope, err = a.Access.Project(r.Context(), kind, id)
					if err != nil {
						return apperr.Forbidden("Acesso não autorizado.")
					}
				}
				if err := p.AuthorizeAccess(r.Context(), a.Access, scope, ability); err != nil {
					return err
				}
			}
			if err := a.checkReferences(r, p, ability, item); err != nil {
				return err
			}
		}
	}
	return nil
}
