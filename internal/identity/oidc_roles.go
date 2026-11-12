package identity

import (
	"strings"

	"pier/internal/access"
	"pier/internal/platform/apperr"
)

const (
	defaultRoleClaim = "groups"
	maxRoleMappings  = 64
)

// validateRoleMapping accepts admin, developer and viewer only. SSO never
// grants owner: a compromised identity provider must not own the instance.
func validateRoleMapping(c OIDCConfig) error {
	if len(c.RoleMappings) > maxRoleMappings || len(c.RoleClaim) > 64 {
		return apperr.Invalid("Mapeamento de papéis do SSO inválido.")
	}
	if c.DefaultRole != "" && !ssoRole(c.DefaultRole) {
		return apperr.Invalid("Mapeamento de papéis do SSO inválido.")
	}
	for value, role := range c.RoleMappings {
		if strings.TrimSpace(value) == "" || len(value) > 255 || !ssoRole(role) {
			return apperr.Invalid("Mapeamento de papéis do SSO inválido.")
		}
	}
	return nil
}

func ssoRole(role string) bool {
	parsed := access.Role(role)
	return parsed.Valid() && parsed != access.Owner
}

// mappedRole returns the strongest role mapped from the claim values, or the
// default role. managed is false when the configuration maps nothing: then
// roles are only managed inside Pier.
func mappedRole(c OIDCConfig, claims map[string]any) (role access.Role, managed bool) {
	if len(c.RoleMappings) == 0 && c.DefaultRole == "" {
		return "", false
	}
	claim := c.RoleClaim
	if claim == "" {
		claim = defaultRoleClaim
	}
	for _, value := range claimValues(claims[claim]) {
		if mapped := access.Role(c.RoleMappings[value]); ssoRole(string(mapped)) && mapped.AtLeast(role) {
			role = mapped
		}
	}
	if role == "" && ssoRole(c.DefaultRole) {
		role = access.Role(c.DefaultRole)
	}
	return role, true
}

// claimValues accepts a single string or a list of strings; anything else
// maps to nothing.
func claimValues(claim any) []string {
	switch typed := claim.(type) {
	case string:
		return []string{typed}
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if value, ok := item.(string); ok {
				values = append(values, value)
			}
		}
		return values
	}
	return nil
}
