package gitprovider

import (
	"context"
	"errors"
	"log/slog"
	"net/url"
	"time"

	"pier/internal/platform/apperr"
)

// WithCredentials scopes the connection token of a linked resource to one
// clone. repository carries the clone user (no password); token is the
// password. ErrNoSelection means the resource has no connection link. The
// token only goes to the clone address, which must stay under the
// connection address (checked again here, not only when linking).
func (c *Connections) WithCredentials(ctx context.Context, kind, id string, fn func(repository, token string) error) error {
	link, found, err := c.linkRecord(ctx, kind, id)
	if err != nil {
		return err
	}
	if !found || link.orphaned {
		return ErrNoSelection
	}
	if err := c.service.requireBoundSource(ctx, kind, id, link.cloneURL); err != nil {
		return err
	}
	record, client, err := c.open(ctx, link.connectionID)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(link.cloneURL)
	if err != nil || !withinBase(link.cloneURL, record.BaseURL) {
		return apperr.Invalid("O endereço de clone vinculado está fora do endereço da conexão. Vincule o repositório de novo.")
	}
	user, password := client.cloneCredentials()
	parsed.User = url.User(user)
	return fn(parsed.String(), password)
}

// Credentials resolves clone credentials for a resource: its GitHub App
// selection first, then its provider connection link. Only when neither
// exists is ErrNoSelection returned (the ordinary clone may proceed).
type Credentials struct {
	App         *Service
	Connections *Connections
}

// WithCredentials implements application.GitCredentials.
func (c Credentials) WithCredentials(ctx context.Context, kind, id string, fn func(repository, token string) error) error {
	err := ErrNoSelection
	if c.App != nil {
		err = c.App.WithCredentials(ctx, kind, id, fn)
	}
	if !errors.Is(err, ErrNoSelection) || c.Connections == nil {
		return err
	}
	return c.Connections.WithCredentials(ctx, kind, id, fn)
}

// DefaultSweepInterval is how often webhooks of unlinked resources are retried.
const DefaultSweepInterval = 5 * time.Minute

// RunSweeper removes the webhooks of deleted or unlinked resources until
// ctx ends. Failures are logged without any token (errors name only the
// provider and the HTTP status).
func (c *Connections) RunSweeper(ctx context.Context, interval time.Duration, logger *slog.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if pending, err := c.Sweep(ctx); err != nil {
			logger.Warn("git connection webhook sweep failed", "error", err)
		} else if pending > 0 {
			logger.Info("git connection webhooks waiting for removal", "pending", pending)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
