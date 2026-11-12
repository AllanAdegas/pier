// Package envvar stores the environment variables of applications, databases
// and stacks, encrypted at rest.
package envvar

import (
	"bufio"
	"context"
	"database/sql"
	"regexp"
	"sort"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

// OwnerType identifies which kind of resource owns a variable.
type OwnerType string

const (
	OwnerApplication OwnerType = "application"
	OwnerDatabase    OwnerType = "database"
	OwnerStack       OwnerType = "stack"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidKey reports whether a name can be used as an environment variable.
func ValidKey(key string) bool { return keyPattern.MatchString(key) }

// Variable is one KEY=value pair of a resource.
type Variable struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	Value       string    `json:"value"`
	BuildTime   bool      `json:"build_time"`
	SecretBuild bool      `json:"secret_build"`
	CreatedAt   time.Time `json:"created_at"`
}

// Owner points to the resource a set of variables belongs to.
type Owner struct {
	Type OwnerType
	ID   string
}

// Store persists variables.
type Store struct {
	db       *sql.DB
	box      *secretbox.Box
	resolver ReferenceResolver
}

// NewStore creates the store.
func NewStore(db *sql.DB, box *secretbox.Box) *Store { return &Store{db: db, box: box} }

// List returns the variables of a resource sorted by key.
func (s *Store) List(ctx context.Context, owner Owner) ([]Variable, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, key, value, build_time, secret_build, created_at FROM env_vars
		WHERE owner_type = ? AND owner_id = ? ORDER BY key`, owner.Type, owner.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	variables := []Variable{}
	for rows.Next() {
		var variable Variable
		var sealed, createdAt string
		if err := rows.Scan(&variable.ID, &variable.Key, &sealed, &variable.BuildTime, &variable.SecretBuild, &createdAt); err != nil {
			return nil, err
		}
		if variable.Value, err = s.box.Open(sealed); err != nil {
			return nil, err
		}
		variable.CreatedAt = sqlite.ParseTime(createdAt)
		variables = append(variables, variable)
	}
	return variables, rows.Err()
}

// Map returns the variables of a resource as a key/value map.
func (s *Store) Map(ctx context.Context, owner Owner) (map[string]string, error) {
	variables, err := s.List(ctx, owner)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(variables))
	for _, variable := range variables {
		values[variable.Key] = variable.Value
	}
	return values, nil
}

// Set creates or replaces one variable.
func (s *Store) Set(ctx context.Context, owner Owner, key, value string, buildTime bool, secretBuild ...bool) error {
	key = strings.TrimSpace(key)
	if !keyPattern.MatchString(key) {
		fields := apperr.Fields{}
		fields.Add("key", "Use letras, números e _ (não pode começar com número).")
		return fields.Err()
	}
	return s.upsert(ctx, s.db, owner, key, value, buildTime, len(secretBuild) > 0 && secretBuild[0])
}

// ReplaceAll makes the stored set equal to the given variables (bulk edit).
func (s *Store) ReplaceAll(ctx context.Context, owner Owner, variables []Variable) error {
	seen := map[string]bool{}
	for _, variable := range variables {
		if !keyPattern.MatchString(variable.Key) {
			return apperr.Invalid("Nome de variável inválido: %q.", variable.Key)
		}
		if seen[variable.Key] {
			return apperr.Invalid("Variável duplicada: %s.", variable.Key)
		}
		seen[variable.Key] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM env_vars WHERE owner_type = ? AND owner_id = ?`,
		owner.Type, owner.ID); err != nil {
		return err
	}
	for _, variable := range variables {
		if err := s.upsert(ctx, tx, owner, variable.Key, variable.Value, variable.BuildTime, variable.SecretBuild); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// SetMissing stores the given defaults without overwriting existing keys.
func (s *Store) SetMissing(ctx context.Context, owner Owner, defaults map[string]string) error {
	existing, err := s.Map(ctx, owner)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(defaults))
	for key := range defaults {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, found := existing[key]; found {
			continue
		}
		if err := s.Set(ctx, owner, key, defaults[key], false); err != nil {
			return err
		}
	}
	return nil
}

// Delete removes one variable by id.
func (s *Store) Delete(ctx context.Context, owner Owner, id string) error {
	err := sqlite.RequireAffected(s.db.ExecContext(ctx,
		`DELETE FROM env_vars WHERE id = ? AND owner_type = ? AND owner_id = ?`, id, owner.Type, owner.ID))
	if err == sqlite.ErrNotFound {
		return apperr.NotFound("Variável")
	}
	return err
}

// DeleteAll removes every variable of a resource (used when it is deleted).
func (s *Store) DeleteAll(ctx context.Context, owner Owner) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM env_vars WHERE owner_type = ? AND owner_id = ?`,
		owner.Type, owner.ID)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func (s *Store) upsert(ctx context.Context, db execer, owner Owner, key, value string, buildTime, secretBuild bool) error {
	sealed, err := s.box.Seal(value)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO env_vars (id, owner_type, owner_id, key, value, build_time, secret_build, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (owner_type, owner_id, key) DO UPDATE SET value = excluded.value, build_time = excluded.build_time, secret_build = excluded.secret_build`,
		idgen.New(), owner.Type, owner.ID, key, sealed, buildTime, secretBuild, sqlite.FormatTime(time.Now()))
	return err
}

// ParseDotenv reads KEY=value lines, ignoring blanks and comments. Values may
// be wrapped in single or double quotes.
func ParseDotenv(content string) ([]Variable, error) {
	variables := []Variable{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !keyPattern.MatchString(key) {
			return nil, apperr.Invalid("Linha %d inválida: esperado CHAVE=valor.", lineNumber)
		}
		variables = append(variables, Variable{Key: key, Value: unquote(strings.TrimSpace(value))})
	}
	return variables, scanner.Err()
}

// FormatDotenv renders variables as a compose-compatible env file.
func FormatDotenv(values map[string]string) []byte {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key + "=" + quoteDotenv(values[key]) + "\n")
	}
	return []byte(builder.String())
}

func unquote(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if first == last && (first == '"' || first == '\'') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

// quoteDotenv single-quotes the value so compose reads it literally; a value
// that itself contains a single quote falls back to escaped double quotes.
func quoteDotenv(value string) string {
	if !strings.Contains(value, "'") {
		return "'" + value + "'"
	}
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, "\n", `\n`)
	return `"` + replacer.Replace(value) + `"`
}
