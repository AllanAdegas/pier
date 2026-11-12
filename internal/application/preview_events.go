package application

import (
	"strconv"

	"pier/internal/notify"
	"pier/internal/platform/i18n"
)

// previewEvent is the notification of a preview deployment. It reuses the
// deployment event types; channels tell previews apart by the resource
// "<application>:preview-<number>" and the title prefix "[preview #<number>]",
// both fixed and never translated.
func previewEvent(applicationName string, preview Preview, status DeploymentStatus, failure string) notify.Event {
	language := i18n.Instance()
	number := strconv.Itoa(preview.Number)
	event := notify.Event{Resource: applicationName + ":preview-" + number}
	marker := "[preview #" + number + "] "
	if status == DeploymentFinished {
		event.Type = notify.DeploymentSucceeded
		event.Title = marker + language.Sprintf("Preview implantado: %s", applicationName)
		event.Message = language.Sprintf("O preview do pull request #%d de %s está no ar em %s.",
			preview.Number, applicationName, preview.URL)
		return event
	}
	event.Type = notify.DeploymentFailed
	event.Title = marker + language.Sprintf("Preview falhou: %s", applicationName)
	event.Message = failure
	return event
}
