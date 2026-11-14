package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/url"

	"pier/internal/platform/apperr"
)

// linkRecord is a stored link row.
type linkRecord struct {
	kind, resourceID, connectionID, repository, cloneURL string
	webhookID, webhookStatus                             string
	hookURL                                              string
	webhookHTTPStatus                                    int
	orphaned                                             bool
}

func (c *Connections) linkRecord(ctx context.Context, kind, id string) (linkRecord, bool, error) {
	link := linkRecord{kind: kind, resourceID: id}
	err := c.service.db.QueryRowContext(ctx, `SELECT connection_id,repository,clone_url,webhook_id,webhook_status,
		webhook_http_status,orphaned,hook_url FROM git_connection_links WHERE kind=? AND resource_id=?`, kind, id).
		Scan(&link.connectionID, &link.repository, &link.cloneURL, &link.webhookID, &link.webhookStatus,
			&link.webhookHTTPStatus, &link.orphaned, &link.hookURL)
	if errors.Is(err, sql.ErrNoRows) {
		return link, false, nil
	}
	return link, err == nil, err
}

func (c *Connections) saveLink(ctx context.Context, link linkRecord) error {
	if link.webhookStatus == "" {
		link.webhookStatus = WebhookNone
	}
	table := "applications"
	if link.kind == KindStack {
		table = "stacks"
	}
	// SQLite serializes this existence check with deletion and its orphan trigger.
	_, err := c.service.db.ExecContext(ctx, `INSERT INTO git_connection_links(kind,resource_id,connection_id,repository,
 clone_url,webhook_id,webhook_status,webhook_http_status,orphaned,hook_url) VALUES(?,?,?,?,?,?,?,?,
 CASE WHEN ? OR NOT EXISTS(SELECT 1 FROM `+table+` WHERE id=?) THEN 1 ELSE 0 END,?)
 ON CONFLICT(kind,resource_id) DO UPDATE SET connection_id=excluded.connection_id,repository=excluded.repository,
 clone_url=excluded.clone_url,webhook_id=excluded.webhook_id,webhook_status=excluded.webhook_status,
 webhook_http_status=excluded.webhook_http_status,orphaned=excluded.orphaned,hook_url=excluded.hook_url`,
		link.kind, link.resourceID, link.connectionID, link.repository, link.cloneURL, link.webhookID,
		link.webhookStatus, link.webhookHTTPStatus, link.orphaned, link.resourceID, link.hookURL)
	return err
}

func (c *Connections) view(ctx context.Context, link linkRecord) ConnectionLink {
	result := ConnectionLink{
		Linked: true, ConnectionID: link.connectionID, Repository: link.repository, CloneURL: link.cloneURL,
		Webhook: link.webhookStatus, WebhookHTTPStatus: link.webhookHTTPStatus,
	}
	if record, err := c.record(ctx, link.connectionID); err == nil {
		result.Provider = record.Provider
	}
	return result
}

// Link returns the connection link of a resource (Linked false when none).
func (c *Connections) Link(ctx context.Context, kind, id string) (ConnectionLink, error) {
	if !validKind(kind) || id == "" {
		return ConnectionLink{}, invalid()
	}
	link, found, err := c.linkRecord(ctx, kind, id)
	if err != nil || !found || link.orphaned {
		return ConnectionLink{Webhook: WebhookNone}, err
	}
	return c.view(ctx, link), nil
}

// Connect links a resource to a repository of a connection and creates (or
// updates) its webhook. The resource source must already be the repository
// clone address. A webhook the provider refuses for lack of permission
// leaves the link in place with the "manual" state: the person then sets
// the webhook up by hand. Linking twice never creates a second webhook.
func (c *Connections) Connect(ctx context.Context, request LinkRequest) (ConnectionLink, error) {
	if err := c.checkLinkRequest(ctx, request); err != nil {
		return ConnectionLink{}, err
	}
	record, client, err := c.open(ctx, request.ConnectionID)
	if err != nil {
		return ConnectionLink{}, err
	}
	if !validRepositoryPath(record.Provider, request.Repository) {
		return ConnectionLink{}, invalidRepository()
	}
	repository, err := client.repository(ctx, request.Repository)
	if status := statusOf(err); status == http.StatusNotFound || status == http.StatusForbidden || status == http.StatusUnauthorized {
		return ConnectionLink{}, apperr.Invalid("O token da conexão não dá acesso a %s.", request.Repository)
	}
	if err != nil {
		return ConnectionLink{}, err
	}
	if !validRepositoryPath(record.Provider, repository.Path) || !withinBase(repository.CloneURL, record.BaseURL) {
		return ConnectionLink{}, apperr.Invalid("O provedor devolveu um endereço de clone fora do endereço da conexão.")
	}
	if !sameRepositoryURL(request.Source, repository.CloneURL) {
		return ConnectionLink{}, apperr.Invalid("A origem do recurso não é %s. Use o endereço de clone do repositório escolhido e vincule de novo.", repository.CloneURL)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	link := linkRecord{kind: request.Kind, resourceID: request.ResourceID, connectionID: record.ID,
		repository: repository.Path, cloneURL: repository.CloneURL}
	previous, found, err := c.linkRecord(ctx, request.Kind, request.ResourceID)
	if err != nil {
		return ConnectionLink{}, err
	}
	if found && !previous.orphaned && previous.connectionID == link.connectionID && previous.repository == link.repository {
		link.webhookID = previous.webhookID
	} else if found {
		if !c.removeHook(ctx, previous) {
			return ConnectionLink{}, apperr.Unavailable("Não foi possível remover o webhook anterior. Tente novamente.")
		}
	}
	// Persist cleanup intent before any remote mutation. A lost create response
	// is recovered by searching the durable callback URL during sweeping.
	link.hookURL = request.Hook.URL
	link.orphaned = true
	if err := c.saveLink(ctx, link); err != nil {
		return ConnectionLink{}, err
	}
	spec := hookSpec{HookTarget: request.Hook, pullRequests: request.Kind == KindApplication}
	link.webhookID, link.webhookStatus, link.webhookHTTPStatus = syncHook(ctx, client, link.repository, link.webhookID, spec)
	link.orphaned = link.webhookStatus == WebhookFailed && link.webhookID == ""
	if err := c.saveLink(ctx, link); err != nil {
		return ConnectionLink{}, err
	}
	return c.view(ctx, link), nil
}

func (c *Connections) checkLinkRequest(ctx context.Context, request LinkRequest) error {
	if !validKind(request.Kind) || request.ResourceID == "" || request.ConnectionID == "" {
		return apperr.Invalid("Escolha uma conexão e um repositório.")
	}
	if err := validHookTarget(request.Hook); err != nil {
		return err
	}
	if err := c.service.requireResource(ctx, request.Kind, request.ResourceID); err != nil {
		return err
	}
	if _, err := c.service.Selection(ctx, request.Kind, request.ResourceID); err == nil {
		return apperr.Conflict("O recurso já está vinculado ao GitHub App. Desvincule-o antes de usar outra conexão.")
	} else if !errors.Is(err, ErrNoSelection) {
		return err
	}
	return nil
}

func validHookTarget(target HookTarget) error {
	parsed, err := url.Parse(target.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil ||
		len(target.Secret) < generatedSecretLimit || len(target.Secret) > maxSecretLength {
		return apperr.Invalid("Endereço de webhook inválido. Confira a URL pública do Pier em Configurações.")
	}
	return nil
}

// syncHook updates the known webhook, else adopts one with the same
// address, else creates it. It returns the hook ID and the link state.
func syncHook(ctx context.Context, client providerClient, repository, known string, spec hookSpec) (string, string, int) {
	id := known
	if id != "" {
		err := client.updateHook(ctx, repository, id, spec)
		if statusOf(err) != http.StatusNotFound {
			return hookOutcome(id, err)
		}
		id = ""
	}
	id, err := client.findHook(ctx, repository, spec.URL)
	if err != nil {
		return hookOutcome("", err)
	}
	if id != "" {
		return hookOutcome(id, client.updateHook(ctx, repository, id, spec))
	}
	id, err = client.createHook(ctx, repository, spec)
	return hookOutcome(id, err)
}

// hookOutcome maps a webhook call result to the stored state. 401 and 403
// mean the token may not manage webhooks: the person falls back to manual.
func hookOutcome(id string, err error) (string, string, int) {
	status := statusOf(err)
	switch {
	case err == nil:
		return id, WebhookActive, 0
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return id, WebhookManual, status
	}
	return id, WebhookFailed, status
}

// RefreshWebhook re-applies the webhook of a linked resource (after its
// secret rotated). Resources without a connection link are left alone.
func (c *Connections) RefreshWebhook(ctx context.Context, kind, id string, hook HookTarget) (ConnectionLink, error) {
	if err := validHookTarget(hook); err != nil {
		return ConnectionLink{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	link, found, err := c.linkRecord(ctx, kind, id)
	if err != nil || !found || link.orphaned {
		return ConnectionLink{Webhook: WebhookNone}, err
	}
	_, client, err := c.open(ctx, link.connectionID)
	if err != nil {
		return ConnectionLink{}, err
	}
	link.hookURL = hook.URL
	link.orphaned = true
	if err := c.saveLink(ctx, link); err != nil {
		return ConnectionLink{}, err
	}
	spec := hookSpec{HookTarget: hook, pullRequests: kind == KindApplication}
	link.webhookID, link.webhookStatus, link.webhookHTTPStatus = syncHook(ctx, client, link.repository, link.webhookID, spec)
	link.orphaned = link.webhookStatus == WebhookFailed && link.webhookID == ""
	if err := c.saveLink(ctx, link); err != nil {
		return ConnectionLink{}, err
	}
	return c.view(ctx, link), nil
}

// Disconnect unlinks a resource. Its webhook is removed now when possible;
// otherwise the sweeper keeps trying (the link is marked orphaned).
func (c *Connections) Disconnect(ctx context.Context, kind, id string) error {
	if !validKind(kind) || id == "" {
		return invalid()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	link, found, err := c.linkRecord(ctx, kind, id)
	if err != nil || !found {
		return err
	}
	if c.removeHook(ctx, link) {
		_, err = c.service.db.ExecContext(ctx, `DELETE FROM git_connection_links WHERE kind=? AND resource_id=?`, kind, id)
		return err
	}
	_, err = c.service.db.ExecContext(ctx, `UPDATE git_connection_links SET orphaned=1 WHERE kind=? AND resource_id=?`, kind, id)
	return err
}

// removeHook deletes the webhook of link from the provider. It reports
// whether nothing is left to retry: removed, already gone, or the
// connection was deleted. Authorization failures retain cleanup state for token rotation.
func (c *Connections) removeHook(ctx context.Context, link linkRecord) bool {
	if link.webhookID == "" && link.hookURL == "" {
		return true
	}
	_, client, err := c.open(ctx, link.connectionID)
	if err != nil {
		return isNotFound(err)
	}
	if link.webhookID == "" {
		link.webhookID, err = client.findHook(ctx, link.repository, link.hookURL)
		if err != nil {
			return false
		}
		if link.webhookID == "" {
			return true
		}
	}
	err = client.deleteHook(ctx, link.repository, link.webhookID)
	switch statusOf(err) {
	case http.StatusNotFound, http.StatusGone:
		return true
	}
	return err == nil
}

func isNotFound(err error) bool {
	failure, ok := apperr.As(err)
	return ok && failure.Kind == apperr.KindNotFound
}

// orphanBatch bounds one sweep.
const orphanBatch = 50

// Sweep removes the webhooks of unlinked or deleted resources and returns
// how many links are still waiting (the provider was unreachable).
func (c *Connections) Sweep(ctx context.Context) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	rows, err := c.service.db.QueryContext(ctx, `SELECT kind,resource_id FROM git_connection_links WHERE orphaned=1 LIMIT ?`, orphanBatch)
	if err != nil {
		return 0, err
	}
	keys := [][2]string{}
	for rows.Next() {
		var key [2]string
		if err := rows.Scan(&key[0], &key[1]); err != nil {
			rows.Close()
			return 0, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	pending := 0
	for _, key := range keys {
		link, found, err := c.linkRecord(ctx, key[0], key[1])
		if err != nil || !found {
			continue
		}
		if !c.removeHook(ctx, link) {
			pending++
			continue
		}
		if _, err := c.service.db.ExecContext(ctx, `DELETE FROM git_connection_links WHERE kind=? AND resource_id=? AND orphaned=1`,
			key[0], key[1]); err != nil {
			return pending, err
		}
	}
	return pending, nil
}
