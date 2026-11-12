package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/sqlite"
	"pier/internal/registry"
)

// maxDestinations bounds a rollout: each server is released in turn.
const maxDestinations = 20

// Rollout states of an application on one server.
const (
	PlacementDeploying      = "deploying"
	PlacementHealthy        = "healthy"
	PlacementFailed         = "failed"
	PlacementRolledBack     = "rolled_back"
	PlacementRollbackFailed = "rollback_failed"
	PlacementInterrupted    = "interrupted"
)

// Placement says where an application is built and which servers run it.
// Destinations always include the application's own server. Without a
// placement (or with only that server and no build server) the application
// is deployed exactly as a single-server application.
type Placement struct {
	BuildServerID string   `json:"build_server_id"`
	RegistryID    string   `json:"registry_id"`
	Repository    string   `json:"repository"`
	Destinations  []string `json:"destinations"`
}

// DestinationStatus is the last known rollout state on one server.
type DestinationStatus struct {
	ServerID      string    `json:"server_id"`
	DeploymentID  string    `json:"deployment_id"`
	Image         string    `json:"image"`
	PreviousImage string    `json:"previous_image"`
	Status        string    `json:"status"`
	Message       string    `json:"message"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// placementPlan is what one deployment of an application must do.
type placementPlan struct {
	Placement
	// servers are the runtime servers, the application's own first.
	servers []string
	// buildServer builds (or pulls) the image.
	buildServer string
	// stale are servers that still run the application but left the placement.
	stale []string
}

// distributed tells whether the image must travel between servers.
func (p placementPlan) distributed() bool {
	for _, id := range p.servers {
		if id != p.buildServer {
			return true
		}
	}
	return false
}

// MultiServer stores placements and the per-server rollout state.
type MultiServer struct {
	db     *sql.DB
	apps   *Store
	guard  DeployGuard
	images *registry.Service
}

// NewMultiServer creates the placement service.
func NewMultiServer(db *sql.DB, apps *Store, guard DeployGuard, images *registry.Service) *MultiServer {
	return &MultiServer{db: db, apps: apps, guard: guard, images: images}
}

// Settings returns the placement of an application (empty when it has none).
func (s *MultiServer) Settings(ctx context.Context, id string) (Placement, error) {
	if _, err := s.apps.Get(ctx, id); err != nil {
		return Placement{}, notFound(err, "Aplicação")
	}
	return s.settings(ctx, id)
}

func (s *MultiServer) settings(ctx context.Context, id string) (Placement, error) {
	p := Placement{Destinations: []string{}}
	var build, registryID sql.NullString
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT build_server_id, registry_id, repository, destinations
		FROM application_placements WHERE application_id = ?`, id).Scan(&build, &registryID, &p.Repository, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	p.BuildServerID, p.RegistryID = build.String, registryID.String
	return p, json.Unmarshal([]byte(raw), &p.Destinations)
}

// Save validates and stores a placement. It takes effect on the next
// deployment, which also removes the application from servers taken out.
func (s *MultiServer) Save(ctx context.Context, id string, p Placement) (Placement, error) {
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return Placement{}, notFound(err, "Aplicação")
	}
	if s.guard != nil {
		if err := s.guard.CheckDeploy(ctx, app.EnvironmentID); err != nil {
			return Placement{}, err
		}
	}
	p = normalizedPlacement(p, app.ServerID)
	if err := s.validate(ctx, app, p); err != nil {
		return Placement{}, err
	}
	raw, _ := json.Marshal(p.Destinations)
	_, err = s.db.ExecContext(ctx, `INSERT INTO application_placements
		(application_id, build_server_id, registry_id, repository, destinations, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(application_id) DO UPDATE SET build_server_id = excluded.build_server_id,
		registry_id = excluded.registry_id, repository = excluded.repository,
		destinations = excluded.destinations, updated_at = excluded.updated_at`,
		id, nullable(p.BuildServerID), nullable(p.RegistryID), p.Repository, string(raw), sqlite.FormatTime(time.Now()))
	if err != nil {
		return Placement{}, err
	}
	return s.settings(ctx, id)
}

// normalizedPlacement trims the input and puts the application's server
// first; a placement that names no destination means "only this server".
func normalizedPlacement(p Placement, primary string) Placement {
	p.BuildServerID = strings.TrimSpace(p.BuildServerID)
	p.RegistryID = strings.TrimSpace(p.RegistryID)
	p.Repository = strings.TrimSpace(p.Repository)
	if p.BuildServerID == primary {
		p.BuildServerID = ""
	}
	destinations := []string{primary}
	for _, id := range p.Destinations {
		if id = strings.TrimSpace(id); id != "" && id != primary {
			destinations = append(destinations, id)
		}
	}
	p.Destinations = destinations
	return p
}

func (s *MultiServer) validate(ctx context.Context, app Application, p Placement) error {
	fields := apperr.Fields{}
	seen := map[string]bool{}
	for _, id := range p.Destinations {
		if seen[id] {
			fields.Add("destinations", "Cada servidor pode aparecer uma vez só.")
			break
		}
		seen[id] = true
		if !s.exists(ctx, "servers", id) {
			fields.Add("destinations", "Um dos servidores escolhidos não existe mais.")
			break
		}
	}
	if len(p.Destinations) > maxDestinations {
		fields.Add("destinations", "Escolha no máximo %d servidores.", maxDestinations)
	}
	if p.BuildServerID != "" && !s.exists(ctx, "servers", p.BuildServerID) {
		fields.Add("build_server_id", "O servidor de build não existe mais.")
	}
	if p.RegistryID != "" && !s.exists(ctx, "registries", p.RegistryID) {
		fields.Add("registry_id", "O registro de imagens não existe mais.")
	}
	if p.Repository != "" && !registry.ValidRepository(p.Repository) {
		fields.Add("repository", "Use letras minúsculas, números, '.', '_', '-' e '/', como empresa/api.")
	}
	if p.RegistryID != "" && p.Repository == "" {
		fields.Add("repository", "Informe o repositório onde as imagens serão enviadas.")
	}
	travels := len(p.Destinations) > 1 || p.BuildServerID != ""
	if travels && app.Config.Build.Pack != PackImage && p.RegistryID == "" {
		fields.Add("registry_id", "Escolha um registro: a imagem construída precisa chegar aos outros servidores.")
	}
	return fields.Err()
}

func (s *MultiServer) exists(ctx context.Context, table, id string) bool {
	var count int
	query := `SELECT COUNT(*) FROM servers WHERE id = ?`
	if table == "registries" {
		query = `SELECT COUNT(*) FROM registries WHERE id = ?`
	}
	return s.db.QueryRowContext(ctx, query, id).Scan(&count) == nil && count == 1
}

// Status returns the rollout state of the application on each server.
func (s *MultiServer) Status(ctx context.Context, id string) ([]DestinationStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id, deployment_id, image, previous_image, status, message, updated_at
		FROM placement_status WHERE application_id = ? ORDER BY server_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DestinationStatus{}
	for rows.Next() {
		var d DestinationStatus
		var updated string
		if err := rows.Scan(&d.ServerID, &d.DeploymentID, &d.Image, &d.PreviousImage, &d.Status, &d.Message, &updated); err != nil {
			return nil, err
		}
		d.UpdatedAt = sqlite.ParseTime(updated)
		result = append(result, d)
	}
	return result, rows.Err()
}

// Recover marks rollouts cut by a restart: the dispatcher already failed
// their deployments, and the next deployment reconciles the servers.
func (s *MultiServer) Recover(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE placement_status SET status = ?, updated_at = ? WHERE status = ?`,
		PlacementInterrupted, sqlite.FormatTime(time.Now()), PlacementDeploying)
	return err
}

func (s *MultiServer) record(ctx context.Context, appID string, status DestinationStatus) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO placement_status
		(application_id, server_id, deployment_id, image, previous_image, status, message, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(application_id, server_id) DO UPDATE SET deployment_id = excluded.deployment_id, image = excluded.image,
		previous_image = excluded.previous_image, status = excluded.status, message = excluded.message, updated_at = excluded.updated_at`,
		appID, status.ServerID, status.DeploymentID, status.Image, status.PreviousImage, status.Status,
		truncateMessage(status.Message), sqlite.FormatTime(time.Now()))
	return err
}

func (s *MultiServer) forget(ctx context.Context, appID string, servers ...string) error {
	for _, id := range servers {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM placement_status WHERE application_id = ? AND server_id = ?`, appID, id); err != nil {
			return err
		}
	}
	return nil
}

// plan resolves what a deployment of app must do. ok is false when the
// application is a plain single-server one.
func (s *MultiServer) plan(ctx context.Context, app Application) (placementPlan, bool, error) {
	p, err := s.settings(ctx, app.ID)
	if err != nil {
		return placementPlan{}, false, err
	}
	statuses, err := s.Status(ctx, app.ID)
	if err != nil {
		return placementPlan{}, false, err
	}
	plan := placementPlan{Placement: p, servers: normalizedPlacement(p, app.ServerID).Destinations, buildServer: app.ServerID}
	if p.BuildServerID != "" {
		plan.buildServer = p.BuildServerID
	}
	for _, status := range statuses {
		if !slices.Contains(plan.servers, status.ServerID) {
			plan.stale = append(plan.stale, status.ServerID)
		}
	}
	active := len(plan.servers) > 1 || plan.buildServer != app.ServerID
	return plan, active || len(plan.stale) > 0, nil
}

// targets are every server the application may have containers on.
func (s *MultiServer) targets(ctx context.Context, app Application) ([]string, error) {
	plan, ok, err := s.plan(ctx, app)
	if err != nil || !ok {
		return []string{app.ServerID}, err
	}
	return append(slices.Clone(plan.servers), plan.stale...), nil
}

// PlacementDestinations returns the servers that run the application, its
// own first, or nil for a single-server application.
func (s *Store) PlacementDestinations(ctx context.Context, app Application) ([]string, error) {
	plan, ok, err := NewMultiServer(s.db, s, nil, nil).plan(ctx, app)
	if err != nil || !ok || len(plan.servers) < 2 {
		return nil, err
	}
	return plan.servers, nil
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func truncateMessage(message string) string {
	const limit = 500
	if len(message) > limit {
		return message[:limit]
	}
	return message
}
