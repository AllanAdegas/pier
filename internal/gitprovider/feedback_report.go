package gitprovider

import (
	"strconv"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// Phase is where a deployment run is, as the Git provider shows it.
type Phase string

const (
	PhaseQueued     Phase = "queued"
	PhaseInProgress Phase = "in_progress"
	PhaseSucceeded  Phase = "succeeded"
	PhaseFailed     Phase = "failed"
	// PhaseCancelled is a run that did not finish (cancelled, interrupted).
	PhaseCancelled Phase = "cancelled"
)

// rank orders the phases of one run: a report of an earlier phase never
// replaces a later one.
func (p Phase) rank() int {
	switch p {
	case PhaseQueued:
		return 1
	case PhaseInProgress:
		return 2
	case PhaseSucceeded, PhaseFailed, PhaseCancelled:
		return 3
	}
	return 0
}

// Finished tells whether the run is over.
func (p Phase) Finished() bool { return p.rank() == 3 }

// state is the commit status equivalent of the phase.
func (p Phase) state() State {
	switch p {
	case PhaseSucceeded:
		return StateSuccess
	case PhaseFailed:
		return StateFailure
	case PhaseCancelled:
		return StateError
	}
	return StatePending
}

// RunReport is the state of one deployment run (an application deployment,
// a preview deployment or a Git stack deploy) on a commit. On GitHub it is a
// check run; elsewhere, and as a fallback, a commit status.
type RunReport struct {
	Kind       string `json:"-"`
	ResourceID string `json:"-"`
	SHA        string `json:"sha"`
	// Context names the check or status, e.g. "pier/web".
	Context string `json:"context"`
	// Run identifies the deployment: a new run starts a new check run.
	Run         string `json:"run"`
	Phase       Phase  `json:"phase"`
	Description string `json:"description"`
	// TargetURL is where the report links to (the deployment in Pier).
	TargetURL string `json:"target_url,omitempty"`
}

// CommentReport keeps one pull request comment of a resource up to date.
type CommentReport struct {
	Kind       string  `json:"-"`
	ResourceID string  `json:"-"`
	Comment    Comment `json:"comment"`
}

func (r RunReport) subject() string { return "run:" + r.SHA + ":" + r.Context }

func (r CommentReport) subject() string {
	return "comment:" + strconv.Itoa(r.Comment.Number) + ":" + r.Comment.Marker
}

func (r RunReport) validate() error {
	if !validKind(r.Kind) || r.ResourceID == "" || r.Run == "" || r.Phase.rank() == 0 {
		return apperr.Invalid("Status de commit inválido.")
	}
	return validStatus(r.status())
}

func (r RunReport) status() CommitStatus {
	return CommitStatus{
		SHA: r.SHA, State: r.Phase.state(), Context: r.Context, Description: r.Description, TargetURL: r.TargetURL,
	}
}

func (r CommentReport) validate() error {
	if !validKind(r.Kind) || r.ResourceID == "" {
		return apperr.Invalid("Comentário de pull request inválido.")
	}
	return validComment(r.Comment)
}

// DeploymentContext is the status/check name of a resource: "pier/<name>",
// short and without control characters.
func DeploymentContext(prefix, name string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, name)
	if runes := []rune(cleaned); len(runes) > 60 {
		cleaned = string(runes[:60])
	}
	return prefix + cleaned
}

// PhaseDescription is the one-line text of a deployment phase, in the
// language of the instance (it is shown on the provider to everyone).
func PhaseDescription(phase Phase) string {
	language := i18n.Instance()
	switch phase {
	case PhaseQueued:
		return language.Text("Commit recebido; aguardando o build no Pier.")
	case PhaseSucceeded:
		return language.Text("Deploy concluído no Pier.")
	case PhaseFailed:
		return language.Text("O deploy falhou. Veja o log no Pier.")
	case PhaseCancelled:
		return language.Text("Deploy cancelado ou interrompido.")
	}
	return language.Text("Deploy em andamento no Pier.")
}
