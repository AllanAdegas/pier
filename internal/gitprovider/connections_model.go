package gitprovider

// Connection is a GitLab, Gitea/Forgejo or Bitbucket Cloud account Pier
// reaches with a stored access token. The token never leaves the server:
// TokenSet tells whether one is stored.
type Connection struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Name     string `json:"name"`
	// BaseURL is the web address of the provider (with its sub-path, for a
	// self-hosted GitLab under one). The token is only sent to its API.
	BaseURL string `json:"base_url"`
	// Workspace narrows Bitbucket listings (needed by workspace access tokens).
	Workspace string `json:"workspace,omitempty"`
	Account   string `json:"account"`
	// Scopes the provider reported for the token; empty when it does not tell.
	Scopes    []string `json:"scopes"`
	TokenSet  bool     `json:"token_set"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at"`
}

// ConnectionInput creates a connection or, on update, replaces its name or
// token (an empty token keeps the stored one). Provider and BaseURL are
// fixed at creation: a token is bound to the address it was saved for.
type ConnectionInput struct {
	Provider  string `json:"provider"`
	Name      string `json:"name"`
	BaseURL   string `json:"base_url"`
	Workspace string `json:"workspace"`
	Token     string `json:"token"`
}

// Account is what the provider says about the token when it is validated.
type Account struct {
	Username string `json:"username"`
	Name     string `json:"name"`
	// Scopes as reported by the provider; ScopesKnown is false when the
	// provider does not expose them (Gitea/Forgejo, Bitbucket app passwords).
	Scopes      []string `json:"scopes"`
	ScopesKnown bool     `json:"scopes_known"`
	// Missing lists required scopes the token lacks (only when known).
	Missing []string `json:"missing"`
}

// ConnectionRepository is a repository visible to a connection.
type ConnectionRepository struct {
	// Path is "group/sub/project" (GitLab) or "owner/name".
	Path          string `json:"path"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	CloneURL      string `json:"clone_url"`
}

// RepositoryPage is one page of repositories (or branches) and whether more follow.
type RepositoryPage struct {
	Items   []ConnectionRepository `json:"items"`
	Page    int                    `json:"page"`
	HasMore bool                   `json:"has_more"`
}

// BranchPage is one page of branch names.
type BranchPage struct {
	Items   []string `json:"items"`
	Page    int      `json:"page"`
	HasMore bool     `json:"has_more"`
}

// Webhook states of a link.
const (
	// WebhookNone: no automatic webhook (not attempted yet).
	WebhookNone = "none"
	// WebhookActive: Pier created (or updated) the webhook on the provider.
	WebhookActive = "active"
	// WebhookManual: the provider refused for lack of permission; the
	// person configures the webhook by hand with the usual instructions.
	WebhookManual = "manual"
	// WebhookFailed: any other failure (provider down, invalid address...).
	WebhookFailed = "failed"
)

// ConnectionLink ties a resource to a repository of a connection.
type ConnectionLink struct {
	Linked       bool   `json:"linked"`
	ConnectionID string `json:"connection_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
	Repository   string `json:"repository,omitempty"`
	CloneURL     string `json:"clone_url,omitempty"`
	Webhook      string `json:"webhook"`
	// WebhookHTTPStatus is the provider answer when the webhook failed.
	WebhookHTTPStatus int `json:"webhook_http_status,omitempty"`
}

// LinkRequest links a resource. Source is the repository its source uses
// now; Hook is where the provider must deliver events and with what secret.
type LinkRequest struct {
	Kind         string
	ResourceID   string
	ConnectionID string
	Repository   string
	Source       string
	Hook         HookTarget
}

// HookTarget is the per-resource webhook receiver and its secret (the
// resource webhook secret, verified by the existing per-provider checks).
type HookTarget struct {
	URL    string
	Secret string
}

// hookSpec is what a provider webhook must look like.
type hookSpec struct {
	HookTarget
	// pullRequests subscribes to PR/MR events too (applications, for previews).
	pullRequests bool
}
