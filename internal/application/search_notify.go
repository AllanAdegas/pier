package application

import "context"

// SearchNotifier tells search engines about the pages of an application
// after a successful deployment. It reports through report and never fails
// the deployment.
type SearchNotifier interface {
	Deployed(ctx context.Context, applicationID string, report func(string))
}

// SetSearchNotifier installs the notifier; nil notifies nobody.
func (d *Deployer) SetSearchNotifier(notifier SearchNotifier) { d.search = notifier }

// notifySearchEngines runs once the deployment, hooks included, succeeded.
// Previews are never announced: they are temporary addresses.
func (d *Deployer) notifySearchEngines(ctx context.Context, deployment Deployment, log *deployLog) {
	if d.search == nil || deployment.Trigger == TriggerPreview {
		return
	}
	d.search.Deployed(ctx, deployment.ApplicationID, func(message string) { log.Info("%s", message) })
}
