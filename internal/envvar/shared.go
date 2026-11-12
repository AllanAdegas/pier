package envvar

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

// SharedScope is where a shared variable lives: a whole project or one
// environment. It is also the namespace applications reference it with.
type SharedScope string

const (
	SharedProject     SharedScope = "project"
	SharedEnvironment SharedScope = "environment"
)

// Valid reports whether the scope is known.
func (s SharedScope) Valid() bool { return s == SharedProject || s == SharedEnvironment }

func (s SharedScope) column() string {
	if s == SharedProject {
		return "project_id"
	}
	return "environment_id"
}

// SharedVariable is a value several applications reference. Secret values
// are sealed like every value and never leave the API (see Masked).
type SharedVariable struct {
	ID        string      `json:"id"`
	Scope     SharedScope `json:"scope"`
	ScopeID   string      `json:"scope_id"`
	Key       string      `json:"key"`
	Value     string      `json:"value"`
	Secret    bool        `json:"secret"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Masked is the variable as the API shows it: a secret has no value.
func (v SharedVariable) Masked() SharedVariable {
	if v.Secret {
		v.Value = ""
	}
	return v
}

// Reference is how an application uses the variable, e.g. ${{project.API_URL}}.
func (v SharedVariable) Reference() string { return SharedReference(v.Scope, v.Key) }

// SharedReference renders the reference to a shared variable.
func SharedReference(scope SharedScope, key string) string {
	return "${{" + string(scope) + "." + key + "}}"
}

// SharedInput creates or changes a shared variable. A nil Value keeps the
// stored one, so a secret can be renamed or re-flagged without resending it.
type SharedInput struct {
	Key    string  `json:"key"`
	Value  *string `json:"value"`
	Secret bool    `json:"secret"`
}

// ListShared returns the shared variables of a project or environment.
func (s *Store) ListShared(ctx context.Context, scope SharedScope, scopeID string) ([]SharedVariable, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, key, value, secret, created_at, updated_at FROM shared_variables
		WHERE `+scope.column()+` = ? ORDER BY key`, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	variables := []SharedVariable{}
	for rows.Next() {
		variable := SharedVariable{Scope: scope, ScopeID: scopeID}
		var sealed, createdAt, updatedAt string
		if err := rows.Scan(&variable.ID, &variable.Key, &sealed, &variable.Secret, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		if variable.Value, err = s.box.Open(sealed); err != nil {
			return nil, err
		}
		variable.CreatedAt, variable.UpdatedAt = sqlite.ParseTime(createdAt), sqlite.ParseTime(updatedAt)
		variables = append(variables, variable)
	}
	return variables, rows.Err()
}

// SetShared creates or replaces a shared variable. changed reports whether
// the value applications receive is different from before.
func (s *Store) SetShared(ctx context.Context, scope SharedScope, scopeID string, input SharedInput) (variable SharedVariable, changed bool, err error) {
	input.Key = strings.TrimSpace(input.Key)
	if err := validateShared(scope, input); err != nil {
		return SharedVariable{}, false, err
	}
	current, found, err := s.sharedByKey(ctx, scope, scopeID, input.Key)
	if err != nil {
		return SharedVariable{}, false, err
	}
	if input.Value == nil && !found {
		fields := apperr.Fields{}
		fields.Add("value", "Informe o valor da variável.")
		return SharedVariable{}, false, fields.Err()
	}
	value := current.Value
	if input.Value != nil {
		value = *input.Value
	}
	sealed, err := s.box.Seal(value)
	if err != nil {
		return SharedVariable{}, false, err
	}
	now := sqlite.FormatTime(time.Now())
	_, err = s.db.ExecContext(ctx, `INSERT INTO shared_variables (id, `+scope.column()+`, key, value, secret, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (`+scope.column()+`, key) WHERE `+scope.column()+` IS NOT NULL
		DO UPDATE SET value = excluded.value, secret = excluded.secret, updated_at = excluded.updated_at`,
		idgen.New(), scopeID, input.Key, sealed, input.Secret, now, now)
	if err != nil {
		return SharedVariable{}, false, err
	}
	variable, _, err = s.sharedByKey(ctx, scope, scopeID, input.Key)
	return variable, !found || current.Value != value, err
}

// DeleteShared removes a shared variable by key.
func (s *Store) DeleteShared(ctx context.Context, scope SharedScope, scopeID, key string) error {
	err := sqlite.RequireAffected(s.db.ExecContext(ctx,
		`DELETE FROM shared_variables WHERE `+scope.column()+` = ? AND key = ?`, scopeID, key))
	if err == sqlite.ErrNotFound {
		return apperr.NotFound("Variável compartilhada")
	}
	return err
}

func (s *Store) sharedByKey(ctx context.Context, scope SharedScope, scopeID, key string) (SharedVariable, bool, error) {
	variables, err := s.ListShared(ctx, scope, scopeID)
	if err != nil {
		return SharedVariable{}, false, err
	}
	for _, variable := range variables {
		if variable.Key == key {
			return variable, true, nil
		}
	}
	return SharedVariable{}, false, nil
}

func validateShared(scope SharedScope, input SharedInput) error {
	fields := apperr.Fields{}
	if !scope.Valid() {
		fields.Add("scope", "Escopo desconhecido: use project ou environment.")
	}
	if !keyPattern.MatchString(input.Key) {
		fields.Add("key", "Use letras, números e _ (não pode começar com número).")
	}
	if input.Value != nil && strings.Contains(*input.Value, "${{") {
		fields.Add("value", "Variáveis compartilhadas não aceitam referências ${{...}}; use a referência na própria aplicação.")
	}
	if input.Value != nil && strings.ContainsRune(*input.Value, '\x00') {
		fields.Add("value", "O valor não pode conter caractere nulo.")
	}
	return fields.Err()
}

// ProjectOf returns the project of an environment ("" when unknown).
func (s *Store) ProjectOf(ctx context.Context, environmentID string) (string, error) {
	var projectID string
	err := s.db.QueryRowContext(ctx, `SELECT project_id FROM environments WHERE id = ?`, environmentID).Scan(&projectID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return projectID, err
}
