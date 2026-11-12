package application

import (
	"context"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/registry"
	"time"
)

func (d *Deployer) SetMultiServer(s *MultiServer, images *registry.Service) {
	d.multi = s
	d.images = images
}
func (s *Service) SetMultiServer(m *MultiServer) { s.multi = m }
func (s *MultiServer) CheckRecovery(ctx context.Context) error {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM placement_status WHERE status NOT IN ('healthy','preserved')`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return apperr.Unavailable("Rollout multisservidor incompleto. Reconcilie os destinos antes de iniciar o Pier.")
	}
	return nil
}

type enginePlacementBuilder struct{ r *placementEngine }

func (b enginePlacementBuilder) BuildPlacement(ctx context.Context, a Application, ex executor.Executor) (string, error) {
	r := b.r
	p, err := r.d.multi.Settings(ctx, a.ID)
	if err != nil {
		return "", err
	}
	_, srv, err := r.d.connector.Executor(ctx, p.BuildServerID)
	if err != nil {
		return "", err
	}
	a.ServerID = p.BuildServerID
	vars, err := r.d.env.ListForDeploy(ctx, envvar.Owner{Type: envvar.OwnerApplication, ID: a.ID}, a.EnvironmentID, p.BuildServerID)
	if err != nil {
		return "", err
	}
	for _, v := range vars {
		r.log.redact(v.Value)
	}
	r.log.redact(srv.ProxyToken)
	j := &job{app: a, deployment: r.deployment, server: srv, ex: ex, log: r.log, env: vars, domains: r.d.domains}
	defer func() {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		j.cleanupBuildDir(c)
	}()
	if r.deployment.RollbackImage != "" {
		return r.deployment.RollbackImage, nil
	}
	image, err := r.d.produceImage(ctx, j)
	if err != nil {
		return "", err
	}
	r.effective = &j.app
	if err = j.checkImageLabels(ctx, image); err != nil {
		return "", err
	}
	return image, nil
}
func (d *Deployer) executePlacement(ctx context.Context, dep Deployment, log *deployLog) error {
	s := d.multi
	s.mu.Lock()
	defer s.mu.Unlock()
	r := &placementEngine{d: d, deployment: dep, log: log, jobs: map[string]*job{}, previous: map[string][]docker.Container{}, files: map[string][]byte{}, backups: map[string]string{}}
	run := NewMultiServer(s.db, s.apps, s.guard, r)
	if err := run.BuildAndRollout(ctx, dep.ApplicationID, enginePlacementBuilder{r}, d.connector, d.images); err != nil {
		return err
	}
	statuses, err := run.Status(ctx, dep.ApplicationID)
	if err != nil {
		return err
	}
	image := statuses[0].Image
	if err = d.store.setDeploymentImage(ctx, dep.ID, image); err != nil {
		return err
	}
	return d.store.setCurrentImage(ctx, dep.ApplicationID, image)
}

func (s *MultiServer) SetRolloutQueue(service *Service) {
	s.enqueue = func(ctx context.Context, id, image string) error {
		app, err := service.Get(ctx, id)
		if err != nil {
			return err
		}
		if err = service.checkDeploy(ctx, app); err != nil {
			return err
		}
		_, err = service.enqueue(ctx, Deployment{ApplicationID: id, Trigger: TriggerRollback, RollbackImage: image})
		return err
	}
}
func (s *Service) placementOperate(ctx context.Context, id, status string, op containerOperation) (bool, error) {
	if s.multi == nil {
		return false, nil
	}
	p, err := s.multi.Settings(ctx, id)
	if err != nil {
		return true, err
	}
	if len(p.Destinations) == 0 {
		return false, nil
	}
	resume, err := s.dispatcher.quiesce(ctx, id)
	if err != nil {
		return true, err
	}
	defer resume()
	s.multi.mu.Lock()
	defer s.multi.mu.Unlock()
	for _, target := range p.Destinations {
		ex, _, e := s.connector.Executor(ctx, target)
		if e != nil {
			return true, e
		}
		containers, e := docker.ListByResource(ctx, ex, id)
		if e != nil {
			return true, e
		}
		if len(containers) == 0 {
			return true, apperr.Conflict("Destino sem containers.")
		}
		if e = op(ctx, ex, containers); e != nil {
			return true, apperr.Unavailable("Operação parcial nos destinos.")
		}
	}
	return true, s.store.SetStatus(ctx, id, status)
}
func (s *Store) PlacementDestinations(ctx context.Context, id string) ([]string, error) {
	p, err := NewMultiServer(s.db, s, nil, nil).Settings(ctx, id)
	return p.Destinations, err
}
