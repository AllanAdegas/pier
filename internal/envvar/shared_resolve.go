package envvar

import (
	"context"
	"regexp"
	"strings"

	"pier/internal/platform/i18n"
)

// sharedReferencePattern matches ${{project.NAME}} and ${{environment.NAME}}.
var sharedReferencePattern = regexp.MustCompile(`\$\{\{(project|environment)\.([A-Za-z_][A-Za-z0-9_]*)\}\}`)

// databaseFields are the fields of a database reference (references.go).
var databaseFields = map[string]bool{"url": true, "host": true, "port": true, "user": true, "password": true, "database": true}

// SharedValues are the shared variables visible to one environment.
type SharedValues struct {
	Project     map[string]string
	Environment map[string]string
}

func (v SharedValues) lookup(scope SharedScope, key string) (string, bool) {
	values := v.Environment
	if scope == SharedProject {
		values = v.Project
	}
	value, found := values[key]
	return value, found
}

// resolveAllReferences is ListForDeploy's resolution: shared variables of the
// application's project/environment, then database references. Shared values
// are only loaded when some variable uses one.
func (s *Store) resolveAllReferences(ctx context.Context, variables []Variable, environmentID, serverID string) ([]Variable, error) {
	shared := SharedValues{}
	if usesShared(variables) {
		var err error
		if shared, err = s.SharedValuesFor(ctx, environmentID); err != nil {
			return nil, err
		}
	}
	return ResolveAll(ctx, variables, shared, environmentID, serverID, s.resolver)
}

func usesShared(variables []Variable) bool {
	for _, variable := range variables {
		if sharedReferencePattern.MatchString(variable.Value) {
			return true
		}
	}
	return false
}

// SharedValuesFor loads the shared variables an environment's applications see.
func (s *Store) SharedValuesFor(ctx context.Context, environmentID string) (SharedValues, error) {
	values := SharedValues{Project: map[string]string{}, Environment: map[string]string{}}
	projectID, err := s.ProjectOf(ctx, environmentID)
	if err != nil {
		return SharedValues{}, err
	}
	for scope, scopeID := range map[SharedScope]string{SharedProject: projectID, SharedEnvironment: environmentID} {
		if scopeID == "" {
			continue
		}
		variables, err := s.ListShared(ctx, scope, scopeID)
		if err != nil {
			return SharedValues{}, err
		}
		target := values.Environment
		if scope == SharedProject {
			target = values.Project
		}
		for _, variable := range variables {
			target[variable.Key] = variable.Value
		}
	}
	return values, nil
}

// ResolveAll resolves every reference of the variables, in this order:
//
//  1. Each value is scanned once, left to right. ${{environment.NAME}} takes
//     NAME from the application's environment and ${{project.NAME}} from its
//     project. Scopes never fall back to each other.
//  2. A project/environment token with no shared variable of that name whose
//     field is a database field (url, host, port, user, password, database)
//     stays a database reference, so a database named "project" keeps working.
//  3. Any other missing shared variable fails before the build, naming the
//     application variable and the reference — never a value.
//  4. The remaining text goes through the database references (references.go).
//
// Resolved values are inserted literally and never scanned again: a shared or
// database value that looks like a reference stays text.
func ResolveAll(ctx context.Context, variables []Variable, shared SharedValues, environmentID, serverID string, resolver ReferenceResolver) ([]Variable, error) {
	result := append([]Variable(nil), variables...)
	for index, variable := range result {
		value, err := resolveValue(ctx, variable, shared, func(literal string) (string, error) {
			if literal == "" {
				return "", nil
			}
			resolved, err := ResolveReferences(ctx, []Variable{{Key: variable.Key, Value: literal}}, environmentID, serverID, resolver)
			if err != nil {
				return "", err
			}
			return resolved[0].Value, nil
		})
		if err != nil {
			return nil, err
		}
		result[index].Value = value
	}
	return result, nil
}

func resolveValue(ctx context.Context, variable Variable, shared SharedValues, databases func(string) (string, error)) (string, error) {
	var out, pending strings.Builder
	flush := func() error {
		resolved, err := databases(pending.String())
		pending.Reset()
		out.WriteString(resolved)
		return err
	}
	last := 0
	for _, match := range sharedReferencePattern.FindAllStringSubmatchIndex(variable.Value, -1) {
		pending.WriteString(variable.Value[last:match[0]])
		last = match[1]
		scope, name := SharedScope(variable.Value[match[2]:match[3]]), variable.Value[match[4]:match[5]]
		value, found := shared.lookup(scope, name)
		switch {
		case found:
			if err := flush(); err != nil {
				return "", err
			}
			out.WriteString(value)
		case databaseFields[name]:
			pending.WriteString(variable.Value[match[0]:match[1]])
		default:
			return "", missingShared(variable.Key, scope, name)
		}
	}
	pending.WriteString(variable.Value[last:])
	if err := flush(); err != nil {
		return "", err
	}
	return out.String(), ctx.Err()
}

func missingShared(key string, scope SharedScope, name string) error {
	if scope == SharedProject {
		return i18n.Errorf("a variável %s usa %s, mas o projeto não tem a variável compartilhada %s; crie-a ou corrija a referência",
			key, SharedReference(scope, name), name)
	}
	return i18n.Errorf("a variável %s usa %s, mas o ambiente não tem a variável compartilhada %s; crie-a ou corrija a referência",
		key, SharedReference(scope, name), name)
}
