package gitprovider

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
)

const (
	maxTokenLength = 4096
	maxNameRunes   = 80
	bitbucketAPIv2 = "https://api.bitbucket.org/2.0"
)

// Connections manages provider connections (GitLab, Gitea/Forgejo and
// Bitbucket Cloud), their repository listings and the resources linked to
// them. It shares the hardened API client of Service.
type Connections struct {
	service *Service
	// bitbucketAPI is the Bitbucket Cloud API; tests point it to a fake.
	bitbucketAPI string
	// mu serializes link changes so linking twice at once cannot create two webhooks.
	mu sync.Mutex
}

// NewConnections creates the connection service over the Git provider service.
func NewConnections(s *Service) *Connections {
	return &Connections{service: s, bitbucketAPI: bitbucketAPIv2}
}

// ConnectionResult is a saved connection and what the provider said about its token.
type ConnectionResult struct {
	Connection Connection `json:"connection"`
	Account    Account    `json:"account"`
}

func connectionNotFound() error { return apperr.NotFound("Conexão Git") }

func providerName(provider string) string {
	switch provider {
	case ProviderGitLab:
		return "GitLab"
	case ProviderGitea:
		return "Gitea"
	}
	return "Bitbucket"
}

func cleanToken(token string) (string, error) {
	value := strings.TrimSpace(token)
	if value == "" || len(value) > maxTokenLength || strings.ContainsAny(value, " \t\r\n\x00") {
		return "", apperr.Invalid("Token de acesso inválido.")
	}
	return value, nil
}

func cleanName(name, fallback string) (string, error) {
	value := strings.TrimSpace(name)
	if value == "" {
		value = fallback
	}
	if utf8.RuneCountInString(value) > maxNameRunes || strings.ContainsAny(value, "\r\n\x00") {
		return "", apperr.Invalid("Nome da conexão inválido.")
	}
	return value, nil
}

// Create validates the token against the provider and stores the connection.
func (c *Connections) Create(ctx context.Context, input ConnectionInput) (ConnectionResult, error) {
	base, err := normalizeBaseURL(input.Provider, input.BaseURL)
	if err != nil {
		return ConnectionResult{}, err
	}
	workspace := strings.TrimSpace(input.Workspace)
	if workspace != "" && (input.Provider != ProviderBitbucket || !validSegment(workspace)) {
		return ConnectionResult{}, apperr.Invalid("Workspace do Bitbucket inválido.")
	}
	token, err := cleanToken(input.Token)
	if err != nil {
		return ConnectionResult{}, err
	}
	name, err := cleanName(input.Name, providerName(input.Provider)+" "+strings.TrimPrefix(base, "https://"))
	if err != nil {
		return ConnectionResult{}, err
	}
	record := connectionRecord{Connection: Connection{
		ID: idgen.New(), Provider: input.Provider, Name: name, BaseURL: base, Workspace: workspace,
	}}
	account, err := c.verify(ctx, &record, token)
	if err != nil {
		return ConnectionResult{}, err
	}
	record.CreatedAt = record.UpdatedAt
	if err := c.insert(ctx, record); err != nil {
		return ConnectionResult{}, err
	}
	return ConnectionResult{Connection: record.Connection, Account: account}, nil
}

// Update renames a connection or replaces its token (validated again).
// Provider and address never change: a token stays bound to its server.
func (c *Connections) Update(ctx context.Context, id string, input ConnectionInput) (ConnectionResult, error) {
	record, err := c.record(ctx, id)
	if err != nil {
		return ConnectionResult{}, err
	}
	if input.Provider != "" && input.Provider != record.Provider ||
		input.BaseURL != "" && !sameBase(record.Provider, input.BaseURL, record.BaseURL) {
		return ConnectionResult{}, apperr.Invalid("O provedor e o endereço de uma conexão não mudam: crie uma nova conexão.")
	}
	if record.Name, err = cleanName(input.Name, record.Name); err != nil {
		return ConnectionResult{}, err
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		if token, err = c.service.box.Open(record.sealedToken); err != nil {
			return ConnectionResult{}, err
		}
	} else if token, err = cleanToken(token); err != nil {
		return ConnectionResult{}, err
	}
	account, err := c.verify(ctx, &record, token)
	if err != nil {
		return ConnectionResult{}, err
	}
	if err := c.update(ctx, record); err != nil {
		return ConnectionResult{}, err
	}
	return ConnectionResult{Connection: record.Connection, Account: account}, nil
}

func sameBase(provider, input, stored string) bool {
	normalized, err := normalizeBaseURL(provider, input)
	return err == nil && normalized == stored
}

// verify asks the provider who the token is, then seals it into record.
func (c *Connections) verify(ctx context.Context, record *connectionRecord, token string) (Account, error) {
	account, err := c.client(*record, token).account(ctx)
	if status := statusOf(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
		return Account{}, apperr.Invalid("O %s recusou o token (HTTP %d): confira o token e as permissões mínimas.", providerName(record.Provider), status)
	}
	if err != nil {
		return Account{}, err
	}
	sealed, err := c.service.box.Seal(token)
	if err != nil {
		return Account{}, err
	}
	record.sealedToken, record.TokenSet = sealed, true
	record.Account = account.Username
	record.Scopes = account.Scopes
	record.UpdatedAt = c.service.now().UTC().Format(time.RFC3339)
	return account, nil
}

// Delete removes a connection no resource uses.
func (c *Connections) Delete(ctx context.Context, id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.record(ctx, id); err != nil {
		return err
	}
	count, err := c.linkCount(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperr.Conflict("A conexão está vinculada a %d recurso(s). Desvincule-os antes de removê-la.", count)
	}
	var pending int
	if err := c.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM git_connection_links WHERE connection_id=?`, id).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return apperr.Conflict("Aguarde a remoção dos webhooks pendentes antes de remover a conexão.")
	}
	_, err = c.service.db.ExecContext(ctx, `DELETE FROM git_connections WHERE id=?`, id)
	return err
}

// client opens the provider API of a connection with token.
func (c *Connections) client(record connectionRecord, token string) providerClient {
	switch record.Provider {
	case ProviderGitLab:
		return newGitLabClient(c.service, record.BaseURL, token)
	case ProviderGitea:
		return newGiteaClient(c.service, record.BaseURL, token, record.Account)
	}
	return newBitbucketClient(c.service, c.bitbucketAPI, token, record.Workspace)
}

// open loads a connection and its API client with the unsealed token.
func (c *Connections) open(ctx context.Context, id string) (connectionRecord, providerClient, error) {
	record, err := c.record(ctx, id)
	if err != nil {
		return record, nil, err
	}
	token, err := c.service.box.Open(record.sealedToken)
	if err != nil {
		return record, nil, err
	}
	return record, c.client(record, token), nil
}

// Repositories lists one page of the repositories a connection sees,
// optionally filtered by query. Clone addresses outside the connection
// address are dropped: the token would never be sent there.
func (c *Connections) Repositories(ctx context.Context, id, query string, page int) (RepositoryPage, error) {
	record, client, err := c.open(ctx, id)
	if err != nil {
		return RepositoryPage{}, err
	}
	result, err := client.repositories(ctx, cleanQuery(query), validPage(page))
	if err != nil {
		return RepositoryPage{}, err
	}
	kept := []ConnectionRepository{}
	for _, item := range result.Items {
		if validRepositoryPath(record.Provider, item.Path) && withinBase(item.CloneURL, record.BaseURL) {
			kept = append(kept, item)
		}
	}
	result.Items = kept
	return result, nil
}

// Branches lists one page of the branches of a repository of a connection.
func (c *Connections) Branches(ctx context.Context, id, repository, query string, page int) (BranchPage, error) {
	record, client, err := c.open(ctx, id)
	if err != nil {
		return BranchPage{}, err
	}
	if !validRepositoryPath(record.Provider, repository) {
		return BranchPage{}, invalidRepository()
	}
	return client.branches(ctx, repository, cleanQuery(query), validPage(page))
}

func invalidRepository() error { return apperr.Invalid("Repositório inválido para esta conexão.") }
