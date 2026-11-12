package stack

import (
	"path"
	"strings"
	"time"

	"pier/internal/application"
)

// DefaultComposePath is the compose file looked for in a repository when none is given.
const DefaultComposePath = "docker-compose.yaml"

// checkoutDirName is the folder of the stack's directory that holds the working tree.
const checkoutDirName = "repo"

// GitSource says where a Git stack comes from and what its last deployment ran.
// The repository address may carry an HTTPS token: it is kept sealed and
// WithoutSecrets hides it.
type GitSource struct {
	Repository string `json:"repository"`
	Branch     string `json:"branch"`
	// Tag, when set, is deployed instead of the branch and push webhooks are ignored.
	Tag string `json:"tag"`
	// BaseDirectory is the folder of the repository the compose file is looked up in.
	BaseDirectory string `json:"base_directory"`
	ComposePath   string `json:"compose_path"`
	PrivateKeyID  string `json:"private_key_id"`
	// WatchPaths limits webhook deployments to pushes touching these globs.
	WatchPaths []string `json:"watch_paths"`
	AutoDeploy bool     `json:"auto_deploy"`
	// AcceptedRisks are the powers over the server the operator allowed the
	// repository's compose file to ask for; PendingRisks are the ones the
	// last deployment was refused for.
	AcceptedRisks     []string   `json:"accepted_risks"`
	PendingRisks      []string   `json:"pending_risks"`
	LastRef           string     `json:"last_ref"`
	LastCommit        string     `json:"last_commit"`
	LastCommitMessage string     `json:"last_commit_message"`
	LastDeployedAt    *time.Time `json:"last_deployed_at"`
}

// GitInput carries the editable fields of a Git source. Empty fields keep the
// default (creation) or the current value (update).
type GitInput struct {
	Repository    string   `json:"repository"`
	Branch        string   `json:"branch"`
	Tag           string   `json:"tag"`
	BaseDirectory string   `json:"base_directory"`
	ComposePath   string   `json:"compose_path"`
	PrivateKeyID  string   `json:"private_key_id"`
	WatchPaths    []string `json:"watch_paths"`
	// AutoDeploy is on unless explicitly turned off.
	AutoDeploy    *bool    `json:"auto_deploy"`
	AcceptedRisks []string `json:"accepted_risks"`
}

// FromGit reports whether the stack runs a compose file taken from a repository.
func (s Stack) FromGit() bool { return s.Git != nil }

// Ref is what the clone checks out: the fixed tag or the branch.
func (g GitSource) Ref() string {
	if g.Tag != "" {
		return g.Tag
	}
	return g.Branch
}

// ComposeFilePath is the compose file relative to the root of the repository.
func (g GitSource) ComposeFilePath() string { return path.Join(g.BaseDirectory, g.ComposePath) }

// WithoutSecrets hides the credentials of the repository address.
func (g GitSource) WithoutSecrets() GitSource {
	g.Repository = application.WithoutCredentials(g.Repository)
	return g
}

// pendingAmong returns the risks that are not in the accepted list.
func (g GitSource) pendingAmong(risks []string) []string {
	pending := []string{}
	for _, risk := range risks {
		accepted := false
		for _, allowed := range g.AcceptedRisks {
			accepted = accepted || allowed == risk
		}
		if !accepted {
			pending = append(pending, risk)
		}
	}
	return pending
}

func cleanBase(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || strings.Contains(value, "\x00") || strings.Contains(value, "..") {
		return value // left as typed so validation reports it
	}
	return strings.Trim(path.Clean("/"+value), "/")
}
