package application

import (
	"context"
	"strconv"
	"time"

	"pier/internal/envvar"
	"pier/internal/platform/apperr"
)

// TriggerPreview builds the head commit of a pull request as an isolated
// preview of the application.
const TriggerPreview Trigger = "preview"

// PreviewEnvOwner scopes the variables that only previews receive. They
// override the application's variables of the same name.
const PreviewEnvOwner envvar.OwnerType = "application_preview"

const (
	defaultMaxPreviews = 5
	maxMaxPreviews     = 20
	defaultPreviewTTL  = 7
	maxPreviewTTL      = 90
)

// PreviewSettings are the opt-in preview rules of one application.
type PreviewSettings struct {
	Enabled bool `json:"enabled"`
	// DomainTemplate renders the preview URL, e.g. "pr-{number}.preview.example.com".
	DomainTemplate string `json:"domain_template"`
	// MaxPreviews caps how many previews exist at once; new ones beyond it are refused.
	MaxPreviews int `json:"max_previews"`
	// AllowForks lets pull requests from forks build on the server. Off by
	// default: a fork author is anyone on the internet.
	AllowForks bool `json:"allow_forks"`
	// TTLDays removes previews without activity for this long.
	TTLDays int `json:"ttl_days"`
}

// DefaultPreviewSettings are the settings of an application that never saved any.
func DefaultPreviewSettings() PreviewSettings {
	return PreviewSettings{MaxPreviews: defaultMaxPreviews, TTLDays: defaultPreviewTTL}
}

// ttl is how long a preview may stay without activity.
func (s PreviewSettings) ttl() time.Duration { return time.Duration(s.TTLDays) * 24 * time.Hour }

// Validate checks the settings, filling defaults for zero limits.
func (s *PreviewSettings) Validate() error {
	if s.MaxPreviews == 0 {
		s.MaxPreviews = defaultMaxPreviews
	}
	if s.TTLDays == 0 {
		s.TTLDays = defaultPreviewTTL
	}
	fields := apperr.Fields{}
	if s.MaxPreviews < 1 || s.MaxPreviews > maxMaxPreviews {
		fields.Add("max_previews", "Use de 1 a %d previews simultâneos.", maxMaxPreviews)
	}
	if s.TTLDays < 1 || s.TTLDays > maxPreviewTTL {
		fields.Add("ttl_days", "Use de 1 a %d dias.", maxPreviewTTL)
	}
	template, err := parseDomainTemplate(s.DomainTemplate)
	switch {
	case err != nil:
		fields.Add("domain_template", "%s", err)
	case s.Enabled && template == "":
		fields.Add("domain_template", "Informe o modelo de domínio dos previews.")
	}
	s.DomainTemplate = template
	return fields.Err()
}

// PreviewStatus is the lifecycle state of a preview.
type PreviewStatus string

const (
	PreviewDeploying  PreviewStatus = "deploying"
	PreviewRunning    PreviewStatus = "running"
	PreviewFailed     PreviewStatus = "failed"
	PreviewDestroying PreviewStatus = "destroying"
)

// Preview is the running copy of one pull request of an application.
type Preview struct {
	// ID is also the docker identity of the preview, distinct from the application's.
	ID               string        `json:"id"`
	ApplicationID    string        `json:"application_id"`
	Number           int           `json:"number"`
	Provider         GitProvider   `json:"provider"`
	Branch           string        `json:"branch"`
	CommitSHA        string        `json:"commit_sha"`
	Title            string        `json:"title"`
	Author           string        `json:"author"`
	Fork             bool          `json:"fork"`
	URL              string        `json:"url"`
	Status           PreviewStatus `json:"status"`
	Image            string        `json:"-"`
	LastDeploymentID string        `json:"last_deployment_id"`
	// LastDeployment is the status of the latest deployment, filled on listing.
	LastDeployment *PreviewDeployment `json:"last_deployment"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	LastActivityAt time.Time          `json:"last_activity_at"`
}

// PreviewDeployment summarizes the latest deployment of a preview.
type PreviewDeployment struct {
	Status     DeploymentStatus `json:"status"`
	CreatedAt  time.Time        `json:"created_at"`
	FinishedAt *time.Time       `json:"finished_at"`
}

// ref is how deployments of this preview are told apart: never a valid Git
// ref, so no tag deployment of the main application can join them.
func (p Preview) ref() string { return previewRef(p.Number) }

func previewRef(number int) string { return "#" + strconv.Itoa(number) }

// PreviewEvent is what a PreviewReporter is told.
type PreviewEvent string

const (
	PreviewEventDeploying PreviewEvent = "deploying"
	PreviewEventReady     PreviewEvent = "ready"
	PreviewEventFailed    PreviewEvent = "failed"
	PreviewEventDestroyed PreviewEvent = "destroyed"
)

// PreviewReport describes one change of a preview for a reporter.
type PreviewReport struct {
	Event        PreviewEvent
	Application  Application
	Preview      Preview
	DeploymentID string
	// Failure is the masked reason of a failed deployment.
	Failure string
}

// PreviewReporter tells the Git provider about previews (e.g. a pull request
// comment or commit status). Implementations must return quickly and do
// network calls asynchronously: they are called from the deployment path.
type PreviewReporter interface {
	ReportPreview(ctx context.Context, report PreviewReport)
}

type noPreviewReporter struct{}

func (noPreviewReporter) ReportPreview(context.Context, PreviewReport) {}
