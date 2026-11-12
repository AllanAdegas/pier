package application

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sync"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/registry"
)

type Placement struct {
	BuildServerID string   `json:"build_server_id"`
	RegistryID    string   `json:"registry_id"`
	Repository    string   `json:"repository"`
	Destinations  []string `json:"destinations"`
}
type DestinationStatus struct {
	ServerID      string `json:"server_id"`
	Image         string `json:"image"`
	PreviousImage string `json:"previous_image"`
	Status        string `json:"status"`
}

// PlacementRuntime is the additive engine hook. Prepare must leave old routes
// and containers intact, Ready checks ALL replicas, Activate switches routes,
// Restore undoes even a partially failed Activate, Discard removes candidates.
// Implementations must render proxy labels/TLS separately for each server.
type PlacementRuntime interface {
	Architecture(context.Context, string) (string, error)
	Prepare(context.Context, Application, string, string) error
	Ready(context.Context, Application, string, string) error
	Activate(context.Context, Application, string, string) error
	Restore(context.Context, Application, string, string) error
	Discard(context.Context, Application, string, string) error
}
type MultiServer struct {
	enqueue func(context.Context, string, string) error
	db      *sql.DB
	apps    *Store
	guard   DeployGuard
	runtime PlacementRuntime
	mu      sync.Mutex
}

func NewMultiServer(db *sql.DB, apps *Store, guard DeployGuard, runtime PlacementRuntime) *MultiServer {
	return &MultiServer{db: db, apps: apps, guard: guard, runtime: runtime}
}
func (s *MultiServer) Settings(ctx context.Context, id string) (Placement, error) {
	var p Placement
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT build_server_id,registry_id,repository,destinations FROM application_placements WHERE application_id=?`, id).Scan(&p.BuildServerID, &p.RegistryID, &p.Repository, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		p.Destinations = []string{}
		return p, nil
	}
	if err != nil {
		return p, err
	}
	err = json.Unmarshal([]byte(raw), &p.Destinations)
	return p, err
}
func (s *MultiServer) Save(ctx context.Context, id string, p Placement) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.guard == nil {
		return apperr.Unavailable("Proteção de ambiente indisponível.")
	}
	if err = s.guard.CheckDeploy(ctx, app.EnvironmentID); err != nil {
		return err
	}
	if p.BuildServerID == "" || p.RegistryID == "" || len(p.Destinations) == 0 || len(p.Destinations) > 20 || !regexp.MustCompile(`^[a-z0-9][a-z0-9/_.-]*$`).MatchString(p.Repository) {
		return apperr.Invalid("Configuração de destinos inválida.")
	}
	seen := map[string]bool{}
	for _, id := range p.Destinations {
		if id == "" || seen[id] {
			return apperr.Invalid("Configuração de destinos inválida.")
		}
		seen[id] = true
		var exists int
		if err = s.db.QueryRowContext(ctx, `SELECT count(*) FROM servers WHERE id=?`, id).Scan(&exists); err != nil {
			return err
		}
		if exists != 1 {
			return apperr.Invalid("Servidor de destino inválido.")
		}
	}
	current, e := s.Settings(ctx, id)
	if e != nil {
		return e
	}
	statuses, e := s.Status(ctx, id)
	if e != nil {
		return e
	}
	if len(statuses) > 0 {
		before, _ := json.Marshal(current)
		after, _ := json.Marshal(p)
		if string(before) != string(after) {
			return apperr.Conflict("Remova os releases antes de alterar os destinos.")
		}
	}
	raw, _ := json.Marshal(p.Destinations)
	_, err = s.db.ExecContext(ctx, `INSERT INTO application_placements VALUES(?,?,?,?,?) ON CONFLICT(application_id) DO UPDATE SET build_server_id=excluded.build_server_id, registry_id=excluded.registry_id,repository=excluded.repository,destinations=excluded.destinations`, id, p.BuildServerID, p.RegistryID, p.Repository, string(raw))
	return err
}
func (s *MultiServer) Status(ctx context.Context, id string) ([]DestinationStatus, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT server_id,image,previous_image,status FROM placement_status WHERE application_id=? ORDER BY server_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []DestinationStatus{}
	for rows.Next() {
		var d DestinationStatus
		if err = rows.Scan(&d.ServerID, &d.Image, &d.PreviousImage, &d.Status); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}
func (s *MultiServer) record(ctx context.Context, id, server, image, previous, status string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO placement_status VALUES(?,?,?,?,?) ON CONFLICT(application_id,server_id) DO UPDATE SET image=excluded.image,previous_image=excluded.previous_image,status=excluded.status`, id, server, image, previous, status)
	return err
}

// Rollout is also the rollback hook: supply a previously recorded digest.
// A failed compensation is persisted as rollback_failed, never reported healthy.
func (s *MultiServer) Rollout(ctx context.Context, id, image string) error {
	if s.enqueue != nil {
		if !registry.Immutable(image) {
			return apperr.Invalid("Use uma imagem por digest.")
		}
		return s.enqueue(ctx, id, image)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rollout(ctx, id, image)
}
func (s *MultiServer) rollout(ctx context.Context, id, image string) error {
	if !registry.Immutable(image) {
		return apperr.Invalid("Use uma imagem por digest.")
	}
	app, err := s.apps.Get(ctx, id)
	if err != nil {
		return err
	}
	if s.guard == nil {
		return apperr.Unavailable("Proteção de ambiente indisponível.")
	}
	if err = s.guard.CheckDeploy(ctx, app.EnvironmentID); err != nil {
		return err
	}
	if s.runtime == nil {
		return apperr.Unavailable("Integração de release multisservidor indisponível.")
	}
	p, err := s.Settings(ctx, id)
	if err != nil {
		return err
	}
	if len(p.Destinations) == 0 {
		return apperr.Invalid("Configuração de destinos inválida.")
	}
	arch, err := s.runtime.Architecture(ctx, p.BuildServerID)
	if err != nil {
		return apperr.Unavailable("Falha ao validar arquitetura.")
	}
	for _, target := range p.Destinations {
		a, e := s.runtime.Architecture(ctx, target)
		if e != nil || a != arch || a == "" {
			return apperr.Invalid("Arquiteturas incompatíveis.")
		}
	}
	statuses, err := s.Status(ctx, id)
	if err != nil {
		return err
	}
	old := map[string]string{}
	for _, d := range statuses {
		if d.Status != "healthy" && d.Status != "preserved" {
			return apperr.Unavailable("Rollout incompleto: reconcilie os destinos.")
		}
		old[d.ServerID] = d.Image
	}
	prepared := []string{}
	activated := map[string]bool{}
	compensate := func() error {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		failed := false
		for i := len(prepared) - 1; i >= 0; i-- {
			target := prepared[i]
			state := "preserved"
			if activated[target] {
				if s.runtime.Restore(cleanup, app, target, old[target]) != nil {
					state = "rollback_failed"
					failed = true
				}
			}
			if state != "rollback_failed" && s.runtime.Discard(cleanup, app, target, image) != nil {
				state = "rollback_failed"
				failed = true
			}
			if s.record(cleanup, id, target, old[target], "", state) != nil {
				failed = true
			}
		}
		if failed {
			return apperr.Unavailable("Rollback parcial: verifique os destinos.")
		}
		return apperr.Unavailable("Deploy falhou; versão anterior preservada.")
	}
	for _, target := range p.Destinations {
		prepared = append(prepared, target)
		if s.record(ctx, id, target, old[target], "", "preparing") != nil {
			return compensate()
		}
		if s.runtime.Prepare(ctx, app, target, image) != nil {
			return compensate()
		}
		if s.runtime.Ready(ctx, app, target, image) != nil {
			return compensate()
		}
	}
	for _, target := range prepared {
		activated[target] = true
		if s.runtime.Activate(ctx, app, target, image) != nil {
			return compensate()
		}
	}
	for _, target := range prepared {
		if s.record(ctx, id, target, image, old[target], "activated") != nil {
			return compensate()
		}
	}
	if retire, ok := s.runtime.(interface {
		Retire(context.Context, Application, string) error
	}); ok {
		for _, target := range prepared {
			if retire.Retire(ctx, app, target) != nil {
				return apperr.Unavailable("Versão ativada; limpeza dos destinos incompleta.")
			}
		}
	}
	for _, target := range prepared {
		if err := s.record(ctx, id, target, image, old[target], "healthy"); err != nil {
			return err
		}
	}
	return nil
}
