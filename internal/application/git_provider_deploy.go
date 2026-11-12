package application

import "context"

// DeploymentEvent is what a DeploymentReporter is told about a deployment
// of the application itself (previews have their own PreviewReporter).
type DeploymentEvent string

const (
	DeploymentEventStarted   DeploymentEvent = "started"
	DeploymentEventSucceeded DeploymentEvent = "succeeded"
	DeploymentEventFailed    DeploymentEvent = "failed"
	DeploymentEventCancelled DeploymentEvent = "cancelled"
)

// DeploymentReport describes a deployment whose commit is known.
type DeploymentReport struct {
	Event       DeploymentEvent
	Application Application
	Deployment  Deployment
}

// DeploymentReporter tells the Git provider about deployments (a commit
// status). Implementations must return at once: they are called from the
// deployment path and must never fail it.
type DeploymentReporter interface {
	ReportDeployment(ctx context.Context, report DeploymentReport)
}

// SetDeploymentReporter installs who reports deployments to the Git provider.
func (d *Deployer) SetDeploymentReporter(reporter DeploymentReporter) {
	d.deploymentReporter = reporter
}

// reportDeploymentStarted runs once the commit is checked out: only then is
// there a commit to put a status on.
func (d *Deployer) reportDeploymentStarted(ctx context.Context, j *job) {
	if d.deploymentReporter == nil || j.deployment.Trigger == TriggerPreview {
		return
	}
	d.deploymentReporter.ReportDeployment(context.WithoutCancel(ctx), DeploymentReport{
		Event: DeploymentEventStarted, Application: j.app, Deployment: j.deployment,
	})
}

// reportDeploymentFinished reports the outcome of a deployment that checked
// out a commit; rollbacks and archives without one report nothing.
func (d *Deployer) reportDeploymentFinished(ctx context.Context, deployment Deployment, status DeploymentStatus) {
	if d.deploymentReporter == nil || deployment.Trigger == TriggerPreview {
		return
	}
	current, err := d.store.GetDeployment(ctx, deployment.ID)
	if err != nil || current.CommitSHA == "" {
		return
	}
	app, err := d.store.Get(ctx, deployment.ApplicationID)
	if err != nil {
		return
	}
	event := DeploymentEventCancelled
	switch status {
	case DeploymentFinished:
		event = DeploymentEventSucceeded
	case DeploymentFailed:
		event = DeploymentEventFailed
	}
	d.deploymentReporter.ReportDeployment(ctx, DeploymentReport{Event: event, Application: app, Deployment: current})
}
