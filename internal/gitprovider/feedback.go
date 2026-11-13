package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strings"

	"pier/internal/platform/apperr"
)

// ErrNoFeedback means the resource reports nothing to its Git provider.
var ErrNoFeedback = errors.New("no Git provider feedback configured")

// Providers that receive commit statuses and pull request comments.
const (
	ProviderNone      = "none"
	ProviderGitHub    = "github"
	ProviderGitLab    = "gitlab"
	ProviderGitea     = "gitea"
	ProviderBitbucket = "bitbucket"
)

// State is the outcome a commit status announces.
type State string

const (
	StatePending State = "pending"
	StateSuccess State = "success"
	StateFailure State = "failure"
	// StateError is a deployment that did not finish (cancelled, interrupted).
	StateError State = "error"
)

// CommitStatus is one status (check) on a commit.
type CommitStatus struct {
	SHA         string
	State       State
	Context     string
	Description string
	TargetURL   string
}

// Comment is a pull request comment Pier keeps up to date. Marker names it:
// the same marker on the same pull request updates the comment in place.
type Comment struct {
	Number int
	Marker string
	Body   string
}

// Reporter talks to the API of one repository.
type Reporter interface {
	SetCommitStatus(ctx context.Context, status CommitStatus) error
	UpsertComment(ctx context.Context, comment Comment) error
}

// Feedback is what a resource reports and through which reporter.
type Feedback struct {
	Provider            string
	CommitStatus        bool
	PullRequestComments bool
	Reporter            Reporter
}

// FeedbackSettings are the per-resource choices. Token is write-only: it is
// accepted on save and never returned (TokenSet tells whether one is stored).
type FeedbackSettings struct {
	Provider            string `json:"provider"`
	CommitStatus        bool   `json:"commit_status"`
	PullRequestComments bool   `json:"pull_request_comments"`
	Token               string `json:"token,omitempty"`
	TokenSet            bool   `json:"token_set"`
	// Saved is false while the defaults apply (on with a GitHub App link).
	Saved bool `json:"saved"`
}

var (
	markerPattern = regexp.MustCompile(`^pier:[a-z0-9:_-]{1,120}$`)
	shaPattern    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type feedbackRow struct {
	provider, sealedToken, origin string
	commitStatus, comments        bool
}

func (s *Service) feedbackRow(ctx context.Context, kind, id string) (feedbackRow, bool, error) {
	var row feedbackRow
	err := s.db.QueryRowContext(ctx, `SELECT provider,encrypted_token,api_origin,commit_status,pull_request_comments
		FROM git_provider_feedback WHERE kind=? AND resource_id=?`, kind, id).
		Scan(&row.provider, &row.sealedToken, &row.origin, &row.commitStatus, &row.comments)
	if errors.Is(err, sql.ErrNoRows) {
		return row, false, nil
	}
	return row, err == nil, err
}

// FeedbackSettings returns the saved choices, or the defaults: everything on
// when the resource is linked to the GitHub App, nothing otherwise.
func (s *Service) FeedbackSettings(ctx context.Context, kind, id string) (FeedbackSettings, error) {
	if !validKind(kind) || id == "" {
		return FeedbackSettings{}, invalid()
	}
	row, saved, err := s.feedbackRow(ctx, kind, id)
	if err != nil {
		return FeedbackSettings{}, err
	}
	if saved {
		return FeedbackSettings{
			Provider: row.provider, CommitStatus: row.commitStatus, PullRequestComments: row.comments,
			TokenSet: row.sealedToken != "", Saved: true,
		}, nil
	}
	if _, err := s.Selection(ctx, kind, id); err == nil {
		return FeedbackSettings{Provider: ProviderGitHub, CommitStatus: true, PullRequestComments: true}, nil
	} else if !errors.Is(err, ErrNoSelection) {
		return FeedbackSettings{}, err
	}
	return FeedbackSettings{Provider: ProviderNone}, nil
}

// SaveFeedback replaces the choices of a resource whose source is
// repository. A token is kept when omitted, as long as the provider and the
// API origin did not change: a token is only ever sent where it was saved for.
func (s *Service) SaveFeedback(ctx context.Context, kind, id, repository string, input FeedbackSettings) (FeedbackSettings, error) {
	if !validKind(kind) || id == "" {
		return FeedbackSettings{}, invalid()
	}
	if err := s.requireResource(ctx, kind, id); err != nil {
		return FeedbackSettings{}, err
	}
	origin, err := feedbackOrigin(input.Provider, repository)
	if err != nil {
		return FeedbackSettings{}, err
	}
	sealed, err := s.feedbackToken(ctx, kind, id, origin, input)
	if err != nil {
		return FeedbackSettings{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO git_provider_feedback
		(kind,resource_id,provider,encrypted_token,api_origin,commit_status,pull_request_comments) VALUES(?,?,?,?,?,?,?)
		ON CONFLICT(kind,resource_id) DO UPDATE SET provider=excluded.provider, encrypted_token=excluded.encrypted_token,
		api_origin=excluded.api_origin, commit_status=excluded.commit_status,
		pull_request_comments=excluded.pull_request_comments`,
		kind, id, input.Provider, sealed, origin, input.CommitStatus, input.PullRequestComments)
	if err != nil {
		return FeedbackSettings{}, err
	}
	return s.FeedbackSettings(ctx, kind, id)
}

// feedbackOrigin is the API origin a token provider is reached at, from the
// repository of the resource; empty for providers without a stored token.
func feedbackOrigin(provider, repository string) (string, error) {
	switch provider {
	case ProviderNone, ProviderGitHub:
		return "", nil
	case ProviderGitLab, ProviderGitea, ProviderBitbucket:
	default:
		return "", apperr.Invalid("Provedor Git desconhecido.")
	}
	location, err := parseRemote(repository)
	if err != nil {
		return "", err
	}
	if provider == ProviderBitbucket && location.host != "bitbucket.org" {
		return "", apperr.Invalid("O retorno do Bitbucket exige um repositório do bitbucket.org.")
	}
	return location.origin, nil
}

// feedbackToken validates the provider choice and returns the sealed token to store.
func (s *Service) feedbackToken(ctx context.Context, kind, id, origin string, input FeedbackSettings) (string, error) {
	token := strings.TrimSpace(input.Token)
	switch input.Provider {
	case ProviderNone:
		return "", nil
	case ProviderGitHub:
		if _, err := s.Selection(ctx, kind, id); err != nil {
			return "", apperr.Invalid("Vincule um repositório do GitHub App antes de ativar o retorno no GitHub.")
		}
		return "", nil
	}
	if token != "" {
		if len(token) > 4096 || strings.ContainsAny(token, " \t\r\n\x00") {
			return "", apperr.Invalid("Token de acesso inválido.")
		}
		return s.box.Seal(token)
	}
	row, saved, err := s.feedbackRow(ctx, kind, id)
	if err != nil {
		return "", err
	}
	if !saved || row.provider != input.Provider || row.origin != origin || row.sealedToken == "" {
		return "", apperr.Invalid("Informe o token de acesso do provedor Git.")
	}
	return row.sealedToken, nil
}

// Feedback resolves how a resource reports, for the repository its source
// uses now. It returns ErrNoFeedback when nothing is to be reported.
func (s *Service) Feedback(ctx context.Context, kind, id, repository string) (Feedback, error) {
	settings, err := s.FeedbackSettings(ctx, kind, id)
	if err != nil {
		return Feedback{}, err
	}
	if settings.Provider == ProviderNone || !settings.CommitStatus && !settings.PullRequestComments {
		return Feedback{}, ErrNoFeedback
	}
	reporter, err := s.reporter(ctx, kind, id, settings.Provider, repository)
	if err != nil {
		return Feedback{}, err
	}
	return Feedback{
		Provider: settings.Provider, CommitStatus: settings.CommitStatus,
		PullRequestComments: settings.PullRequestComments, Reporter: reporter,
	}, nil
}

func (s *Service) reporter(ctx context.Context, kind, id, provider, repository string) (Reporter, error) {
	if provider == ProviderGitHub {
		selection, err := s.Selection(ctx, kind, id)
		if err != nil {
			return nil, err
		}
		return &githubReporter{service: s, selection: selection}, nil
	}
	location, err := parseRemote(repository)
	if err != nil {
		return nil, err
	}
	token, err := s.providerToken(ctx, kind, id, provider, location.origin)
	if err != nil {
		return nil, err
	}
	if token == "" {
		return nil, ErrNoFeedback
	}
	switch provider {
	case ProviderGitLab:
		return newGitLabReporter(s, location, token)
	case ProviderGitea:
		return newGiteaReporter(s, location, token)
	case ProviderBitbucket:
		return newBitbucketReporter(s, location, token)
	}
	return nil, ErrNoFeedback
}

// providerToken opens the stored token only if it was saved for this
// provider and API origin, so a token never reaches another server (for
// instance after the source repository moved). Empty means none applies.
func (s *Service) providerToken(ctx context.Context, kind, id, provider, origin string) (string, error) {
	row, saved, err := s.feedbackRow(ctx, kind, id)
	if err != nil || !saved || row.provider != provider || row.origin != origin || row.sealedToken == "" {
		return "", err
	}
	return s.box.Open(row.sealedToken)
}

// validStatus rejects what no provider would accept, before any call.
func validStatus(status CommitStatus) error {
	if !shaPattern.MatchString(status.SHA) || status.Context == "" || len(status.Context) > 100 {
		return apperr.Invalid("Status de commit inválido.")
	}
	switch status.State {
	case StatePending, StateSuccess, StateFailure, StateError:
		return nil
	}
	return apperr.Invalid("Status de commit inválido.")
}

func validComment(comment Comment) error {
	if comment.Number <= 0 || !markerPattern.MatchString(comment.Marker) || len(comment.Body) > 30000 {
		return apperr.Invalid("Comentário de pull request inválido.")
	}
	return nil
}

// markedBody is the comment text with its hidden marker on the first line.
func markedBody(comment Comment) string {
	return markerLine(comment.Marker) + "\n" + comment.Body
}

func markerLine(marker string) string { return "<!-- " + marker + " -->" }

func hasMarker(body, marker string) bool {
	return strings.HasPrefix(body, markerLine(marker)+"\n") || body == markerLine(marker)
}

// truncate shortens a description to what the provider stores.
func truncate(text string, limit int) string {
	if runes := []rune(text); len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}
