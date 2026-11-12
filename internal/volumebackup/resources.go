package volumebackup

import (
	"context"

	"pier/internal/application"
	"pier/internal/platform/apperr"
	"pier/internal/platform/oplog"
	"pier/internal/stack"
)

type applicationSource interface {
	Get(ctx context.Context, id string) (application.Application, error)
	Deployments(ctx context.Context, applicationID string) ([]application.Deployment, error)
}

type stackSource interface {
	Get(ctx context.Context, id string) (stack.Stack, error)
	Operation(id string) oplog.Snapshot
}

// ResourceDirectory resolves applications and stacks for the service.
type ResourceDirectory struct {
	applications applicationSource
	stacks       stackSource
}

// NewResourceDirectory adapts the application and stack services.
func NewResourceDirectory(applications applicationSource, stacks stackSource) *ResourceDirectory {
	return &ResourceDirectory{applications: applications, stacks: stacks}
}

// Resolve implements Resources.
func (d *ResourceDirectory) Resolve(ctx context.Context, kind, id string) (Resource, error) {
	switch kind {
	case KindApplication:
		app, err := d.applications.Get(ctx, id)
		return Resource{Kind: kind, ID: app.ID, Name: app.Name, ServerID: app.ServerID}, err
	case KindStack:
		item, err := d.stacks.Get(ctx, id)
		return Resource{Kind: kind, ID: item.ID, Name: item.Name, ServerID: item.ServerID}, err
	}
	return Resource{}, apperr.NotFound("Recurso")
}

// EnsureIdle implements Resources.
func (d *ResourceDirectory) EnsureIdle(ctx context.Context, kind, id string) error {
	switch kind {
	case KindApplication:
		return d.ensureNoDeployment(ctx, id)
	case KindStack:
		if d.stacks.Operation(id).Running {
			return apperr.Conflict("Aguarde a operação da stack terminar.")
		}
	}
	return nil
}

func (d *ResourceDirectory) ensureNoDeployment(ctx context.Context, id string) error {
	deployments, err := d.applications.Deployments(ctx, id)
	if err != nil {
		return err
	}
	for _, deployment := range deployments {
		if deployment.Status == application.DeploymentQueued || deployment.Status == application.DeploymentInProgress {
			return apperr.Conflict("Aguarde o deploy da aplicação terminar.")
		}
	}
	return nil
}
