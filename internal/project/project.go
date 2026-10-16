// Package project is the bounded context that organizes resources:
// a project groups environments, and an environment groups resources.
package project

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

// DefaultEnvironment is created together with every project.
const DefaultEnvironment = "production"

var environmentNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Project groups the environments of one product.
type Project struct {
	ID           string        `json:"id"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	CreatedAt    time.Time     `json:"created_at"`
	Environments []Environment `json:"environments"`
}

// Environment is an isolated stage (production, staging…) of a project.
type Environment struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Service implements the project use cases.
type Service struct {
	db *sql.DB
}

// NewService creates the service.
func NewService(db *sql.DB) *Service { return &Service{db: db} }

// List returns every project with its environments.
func (s *Service) List(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	projects := []Project{}
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		projects = append(projects, project)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range projects {
		if projects[i].Environments, err = s.environments(ctx, projects[i].ID); err != nil {
			return nil, err
		}
	}
	return projects, nil
}

// Get returns one project with its environments.
func (s *Service) Get(ctx context.Context, id string) (Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, description, created_at FROM projects WHERE id = ?`, id)
	project, err := scanProject(row)
	if err != nil {
		return Project{}, notFound(err, "Projeto")
	}
	project.Environments, err = s.environments(ctx, id)
	return project, err
}

// Create registers a project with the default environment.
func (s *Service) Create(ctx context.Context, name, description string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		fields := apperr.Fields{}
		fields.Add("name", "Informe o nome do projeto.")
		return Project{}, fields.Err()
	}
	project := Project{ID: idgen.New(), Name: name, Description: strings.TrimSpace(description), CreatedAt: time.Now()}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Project{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects (id, name, description, created_at) VALUES (?, ?, ?, ?)`,
		project.ID, project.Name, project.Description, sqlite.FormatTime(project.CreatedAt)); err != nil {
		return Project{}, err
	}
	environment := Environment{ID: idgen.New(), ProjectID: project.ID, Name: DefaultEnvironment, CreatedAt: project.CreatedAt}
	if _, err := tx.ExecContext(ctx, `INSERT INTO environments (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		environment.ID, environment.ProjectID, environment.Name, sqlite.FormatTime(environment.CreatedAt)); err != nil {
		return Project{}, err
	}
	project.Environments = []Environment{environment}
	return project, tx.Commit()
}

// Update renames a project.
func (s *Service) Update(ctx context.Context, id, name, description string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		fields := apperr.Fields{}
		fields.Add("name", "Informe o nome do projeto.")
		return Project{}, fields.Err()
	}
	err := sqlite.RequireAffected(s.db.ExecContext(ctx, `UPDATE projects SET name = ?, description = ? WHERE id = ?`,
		name, strings.TrimSpace(description), id))
	if err != nil {
		return Project{}, notFound(err, "Projeto")
	}
	return s.Get(ctx, id)
}

// Delete removes an empty project.
func (s *Service) Delete(ctx context.Context, id string) error {
	project, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	for _, environment := range project.Environments {
		if err := s.requireEmpty(ctx, environment.ID); err != nil {
			return err
		}
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id))
}

// CreateEnvironment adds a stage to a project.
func (s *Service) CreateEnvironment(ctx context.Context, projectID, name string) (Environment, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !environmentNamePattern.MatchString(name) {
		fields := apperr.Fields{}
		fields.Add("name", "Use letras minúsculas, números e hífens (até 40 caracteres).")
		return Environment{}, fields.Err()
	}
	project, err := s.Get(ctx, projectID)
	if err != nil {
		return Environment{}, err
	}
	for _, existing := range project.Environments {
		if existing.Name == name {
			return Environment{}, apperr.Conflict("O projeto já possui o ambiente %q.", name)
		}
	}
	environment := Environment{ID: idgen.New(), ProjectID: projectID, Name: name, CreatedAt: time.Now()}
	_, err = s.db.ExecContext(ctx, `INSERT INTO environments (id, project_id, name, created_at) VALUES (?, ?, ?, ?)`,
		environment.ID, environment.ProjectID, environment.Name, sqlite.FormatTime(environment.CreatedAt))
	return environment, err
}

// GetEnvironment returns one environment.
func (s *Service) GetEnvironment(ctx context.Context, id string) (Environment, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, project_id, name, created_at FROM environments WHERE id = ?`, id)
	environment, err := scanEnvironment(row)
	return environment, notFound(err, "Ambiente")
}

// DeleteEnvironment removes an empty environment; a project keeps at least one.
func (s *Service) DeleteEnvironment(ctx context.Context, id string) error {
	environment, err := s.GetEnvironment(ctx, id)
	if err != nil {
		return err
	}
	siblings, err := s.environments(ctx, environment.ProjectID)
	if err != nil {
		return err
	}
	if len(siblings) <= 1 {
		return apperr.Conflict("O projeto precisa de pelo menos um ambiente.")
	}
	if err := s.requireEmpty(ctx, id); err != nil {
		return err
	}
	return sqlite.RequireAffected(s.db.ExecContext(ctx, `DELETE FROM environments WHERE id = ?`, id))
}

func (s *Service) requireEmpty(ctx context.Context, environmentID string) error {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM applications WHERE environment_id = ?1) +
		(SELECT COUNT(*) FROM databases WHERE environment_id = ?1) +
		(SELECT COUNT(*) FROM stacks WHERE environment_id = ?1)`, environmentID).Scan(&count)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperr.Conflict("Ainda existem %d recurso(s) neste ambiente. Remova-os antes.", count)
	}
	return nil
}

func (s *Service) environments(ctx context.Context, projectID string) ([]Environment, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, project_id, name, created_at FROM environments WHERE project_id = ? ORDER BY created_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	environments := []Environment{}
	for rows.Next() {
		environment, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		environments = append(environments, environment)
	}
	return environments, rows.Err()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProject(row rowScanner) (Project, error) {
	var project Project
	var createdAt string
	if err := row.Scan(&project.ID, &project.Name, &project.Description, &createdAt); err != nil {
		return Project{}, err
	}
	project.CreatedAt = sqlite.ParseTime(createdAt)
	return project, nil
}

func scanEnvironment(row rowScanner) (Environment, error) {
	var environment Environment
	var createdAt string
	if err := row.Scan(&environment.ID, &environment.ProjectID, &environment.Name, &createdAt); err != nil {
		return Environment{}, err
	}
	environment.CreatedAt = sqlite.ParseTime(createdAt)
	return environment, nil
}

func notFound(err error, what string) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound(what)
	}
	return err
}
