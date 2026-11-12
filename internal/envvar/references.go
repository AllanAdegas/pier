package envvar

import (
	"context"
	"regexp"
	"strings"

	"pier/internal/platform/i18n"
)

// ReferenceResolver resolves one database field within the deployment's placement.
// Implementations must enforce environment boundaries before reading credentials.
type ReferenceResolver interface {
	Resolve(ctx context.Context, environmentID, serverID, database, field string) (string, error)
}

// SetReferenceResolver installs the adapter at startup, before deployments start.
func (s *Store) SetReferenceResolver(resolver ReferenceResolver) { s.resolver = resolver }

var referencePattern = regexp.MustCompile(`\$\{\{([^{}]+)\.([a-z]+)\}\}`)

// ListForDeploy resolves a copy; API listings and stored values retain references.
func (s *Store) ListForDeploy(ctx context.Context, owner Owner, environmentID, serverID string) ([]Variable, error) {
	variables, err := s.List(ctx, owner)
	if err != nil {
		return nil, err
	}
	return ResolveReferences(ctx, variables, environmentID, serverID, s.resolver)
}

// ResolveReferences performs a single pass so credentials containing reference-like
// text are treated literally. No resolved secret is included in an error.
func ResolveReferences(ctx context.Context, variables []Variable, environmentID, serverID string, resolver ReferenceResolver) ([]Variable, error) {
	result := append([]Variable(nil), variables...)
	for i, variable := range result {
		matches := referencePattern.FindAllStringSubmatchIndex(variable.Value, -1)
		var value strings.Builder
		last := 0
		for _, match := range matches {
			prefix := variable.Value[last:match[0]]
			if strings.Contains(prefix, "${{") {
				return nil, i18n.Errorf("referência de banco inválida na variável %s", variable.Key)
			}
			value.WriteString(prefix)
			name, field := variable.Value[match[2]:match[3]], variable.Value[match[4]:match[5]]
			switch field {
			case "url", "host", "port", "user", "password", "database":
			default:
				return nil, i18n.Errorf("campo de referência de banco desconhecido: %s", field)
			}
			if resolver == nil {
				return nil, i18n.Errorf("o resolvedor de referências de banco não está configurado")
			}
			resolved, err := resolver.Resolve(ctx, environmentID, serverID, name, field)
			if err != nil {
				return nil, i18n.Errorf("não foi possível resolver a referência %s.%s: %w", name, field, err)
			}
			value.WriteString(resolved)
			last = match[1]
		}
		tail := variable.Value[last:]
		if strings.Contains(tail, "${{") {
			return nil, i18n.Errorf("referência de banco inválida na variável %s", variable.Key)
		}
		value.WriteString(tail)
		result[i].Value = value.String()
	}
	return result, nil
}
