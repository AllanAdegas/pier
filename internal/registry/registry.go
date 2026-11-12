// Package registry keeps container registry credentials (sealed at rest) and
// runs the Docker pushes and pulls that need them, without ever placing a
// secret in a command line, a log or a response.
package registry

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

const (
	maxPasswordLength = 16 * 1024
	maxNameLength     = 64
	maxUsernameLength = 255
)

// Registry is what callers may see of a registry: never its password.
type Registry struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	CreatedAt time.Time `json:"created_at"`
}

// Input creates or edits a registry. On edit an empty password keeps the
// stored one.
type Input struct {
	Name     string `json:"name"`
	Host     string `json:"host"`
	Username string `json:"username"`
	Password string `json:"password"`
}

// credential is a registry with its opened password, used only in memory.
type credential struct {
	host, username, password string
}

// Service implements the registry use cases.
type Service struct {
	db     *sql.DB
	box    *secretbox.Box
	client *http.Client
}

// New creates the service.
func New(db *sql.DB, box *secretbox.Box) *Service {
	return &Service{db: db, box: box, client: newHTTPClient()}
}

// SetHTTPClient replaces the client that talks to registry APIs (tests).
func (s *Service) SetHTTPClient(client *http.Client) { s.client = client }

// List returns every registry, without credentials.
func (s *Service) List(ctx context.Context) ([]Registry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, host, username, created_at FROM registries ORDER BY name, host`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	registries := []Registry{}
	for rows.Next() {
		registry, err := scanRegistry(rows)
		if err != nil {
			return nil, err
		}
		registries = append(registries, registry)
	}
	return registries, rows.Err()
}

// Get returns one registry, without credentials.
func (s *Service) Get(ctx context.Context, id string) (Registry, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, host, username, created_at FROM registries WHERE id = ?`, id)
	registry, err := scanRegistry(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Registry{}, apperr.NotFound("Registro de imagens")
	}
	return registry, err
}

// Create stores a registry. One credential per host keeps the choice of
// credential for a pull unambiguous.
func (s *Service) Create(ctx context.Context, input Input) (Registry, error) {
	input = normalized(input)
	if err := validate(input, true); err != nil {
		return Registry{}, err
	}
	sealed, err := s.box.Seal(input.Password)
	if err != nil {
		return Registry{}, err
	}
	id := idgen.New()
	_, err = s.db.ExecContext(ctx, `INSERT INTO registries (id, name, host, username, credential, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Host, input.Username, sealed, sqlite.FormatTime(time.Now()))
	if err != nil {
		return Registry{}, duplicateHost(err)
	}
	return s.Get(ctx, id)
}

// Update edits a registry; an empty password keeps the stored one.
func (s *Service) Update(ctx context.Context, id string, input Input) (Registry, error) {
	if _, err := s.Get(ctx, id); err != nil {
		return Registry{}, err
	}
	input = normalized(input)
	if err := validate(input, false); err != nil {
		return Registry{}, err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE registries SET name = ?, host = ?, username = ? WHERE id = ?`,
		input.Name, input.Host, input.Username, id)
	if err != nil {
		return Registry{}, duplicateHost(err)
	}
	if input.Password != "" {
		sealed, err := s.box.Seal(input.Password)
		if err != nil {
			return Registry{}, err
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE registries SET credential = ? WHERE id = ?`, sealed, id); err != nil {
			return Registry{}, err
		}
	}
	return s.Get(ctx, id)
}

// Delete removes a registry no application depends on.
func (s *Service) Delete(ctx context.Context, id string) error {
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	var users int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM application_placements WHERE registry_id = ?`, id).Scan(&users); err != nil {
		return err
	}
	if users > 0 {
		return apperr.Conflict("Este registro é usado por %d aplicação(ões). Troque o registro delas antes de removê-lo.", users)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM registries WHERE id = ?`, id)
	return err
}

// credentialByID opens the credential of one registry.
func (s *Service) credentialByID(ctx context.Context, id string) (credential, error) {
	return s.openCredential(s.db.QueryRowContext(ctx, `SELECT host, username, credential FROM registries WHERE id = ?`, id))
}

// credentialForHost opens the credential stored for a host, if any.
func (s *Service) credentialForHost(ctx context.Context, host string) (credential, bool, error) {
	found, err := s.openCredential(s.db.QueryRowContext(ctx, `SELECT host, username, credential FROM registries WHERE host = ?`, host))
	if errors.Is(err, sql.ErrNoRows) {
		return credential{}, false, nil
	}
	return found, err == nil, err
}

func (s *Service) openCredential(row *sql.Row) (credential, error) {
	var found credential
	var sealed string
	if err := row.Scan(&found.host, &found.username, &sealed); err != nil {
		return credential{}, err
	}
	password, err := s.box.Open(sealed)
	if err != nil {
		return credential{}, apperr.Unavailable("Não foi possível abrir a credencial do registro %s.", found.host)
	}
	found.password = password
	return found, nil
}

type scanner interface{ Scan(dest ...any) error }

func scanRegistry(row scanner) (Registry, error) {
	var registry Registry
	var created string
	if err := row.Scan(&registry.ID, &registry.Name, &registry.Host, &registry.Username, &created); err != nil {
		return Registry{}, err
	}
	registry.CreatedAt = sqlite.ParseTime(created)
	return registry, nil
}

func normalized(input Input) Input {
	input.Name = strings.TrimSpace(input.Name)
	input.Host = strings.ToLower(strings.TrimSpace(input.Host))
	input.Host = strings.TrimSuffix(strings.TrimPrefix(input.Host, "https://"), "/")
	if dockerHubAliases[input.Host] {
		input.Host = DockerHub
	}
	input.Username = strings.TrimSpace(input.Username)
	return input
}

func validate(input Input, creating bool) error {
	fields := apperr.Fields{}
	if input.Name == "" || len(input.Name) > maxNameLength {
		fields.Add("name", "Informe um nome de até %d caracteres.", maxNameLength)
	}
	if !validHost(input.Host) {
		fields.Add("host", "Informe o host do registro, como ghcr.io ou registry.gitlab.com.")
	}
	if input.Username == "" || len(input.Username) > maxUsernameLength || strings.ContainsFunc(input.Username, invalidUserRune) {
		fields.Add("username", "Informe o usuário do registro, sem espaços nem dois-pontos.")
	}
	if (creating && input.Password == "") || len(input.Password) > maxPasswordLength || strings.ContainsFunc(input.Password, unicode.IsControl) {
		fields.Add("password", "Informe a senha ou o token de acesso do registro.")
	}
	return fields.Err()
}

func invalidUserRune(r rune) bool { return r == ':' || unicode.IsSpace(r) || unicode.IsControl(r) }

func duplicateHost(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return apperr.Conflict("Já existe um registro para este host. Edite o existente.")
	}
	return err
}
