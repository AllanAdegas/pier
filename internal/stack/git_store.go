package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
)

const gitColumns = `stack_id, repository, branch, tag, base_directory, compose_path, private_key_id, watch_paths,
	auto_deploy, accepted_risks, pending_risks, last_ref, last_commit, last_commit_message, last_deployed_at`

// gitStore keeps the Git sources of stacks. The repository address and the
// webhook secret are sealed.
type gitStore struct {
	db  *sql.DB
	box *secretbox.Box
}

func (g gitStore) available() error {
	if g.box == nil {
		return apperr.Unavailable("Stacks do Git não estão disponíveis nesta instância.")
	}
	return nil
}

// insert stores a new source and its webhook secret.
func (g gitStore) insert(ctx context.Context, stackID string, source GitSource, secret string) error {
	repository, err := g.box.Seal(source.Repository)
	if err != nil {
		return err
	}
	sealedSecret, err := g.box.Seal(secret)
	if err != nil {
		return err
	}
	_, err = g.db.ExecContext(ctx, `INSERT INTO stack_git_sources
		(stack_id, repository, branch, tag, base_directory, compose_path, private_key_id, webhook_secret, watch_paths,
		 auto_deploy, accepted_risks) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stackID, repository, source.Branch, source.Tag, source.BaseDirectory, source.ComposePath,
		source.PrivateKeyID, sealedSecret, jsonList(source.WatchPaths), source.AutoDeploy, jsonList(source.AcceptedRisks))
	return err
}

// update replaces the editable settings of a source.
func (g gitStore) update(ctx context.Context, stackID string, source GitSource) error {
	repository, err := g.box.Seal(source.Repository)
	if err != nil {
		return err
	}
	_, err = g.db.ExecContext(ctx, `UPDATE stack_git_sources SET repository = ?, branch = ?, tag = ?, base_directory = ?,
		compose_path = ?, private_key_id = ?, watch_paths = ?, auto_deploy = ?, accepted_risks = ? WHERE stack_id = ?`,
		repository, source.Branch, source.Tag, source.BaseDirectory, source.ComposePath, source.PrivateKeyID,
		jsonList(source.WatchPaths), source.AutoDeploy, jsonList(source.AcceptedRisks), stackID)
	return err
}

// recordDeployment remembers what the last deployment checked out.
func (g gitStore) recordDeployment(ctx context.Context, stackID, ref, sha, message string) error {
	_, err := g.db.ExecContext(ctx, `UPDATE stack_git_sources SET last_ref = ?, last_commit = ?,
		last_commit_message = ?, last_deployed_at = ?, pending_risks = '[]' WHERE stack_id = ?`,
		ref, sha, message, sqlite.FormatTime(time.Now()), stackID)
	return err
}

// recordPendingRisks remembers what a refused deployment asked for.
func (g gitStore) recordPendingRisks(ctx context.Context, stackID string, risks []string) error {
	_, err := g.db.ExecContext(ctx, `UPDATE stack_git_sources SET pending_risks = ? WHERE stack_id = ?`,
		jsonList(risks), stackID)
	return err
}

func (g gitStore) webhookSecret(ctx context.Context, stackID string) (string, error) {
	var sealed string
	err := g.db.QueryRowContext(ctx, `SELECT webhook_secret FROM stack_git_sources WHERE stack_id = ?`, stackID).Scan(&sealed)
	if errors.Is(err, sql.ErrNoRows) {
		return "", apperr.NotFound("Origem Git da stack")
	}
	if err != nil {
		return "", err
	}
	return g.box.Open(sealed)
}

func (g gitStore) setWebhookSecret(ctx context.Context, stackID, secret string) error {
	sealed, err := g.box.Seal(secret)
	if err != nil {
		return err
	}
	return sqlite.RequireAffected(g.db.ExecContext(ctx,
		`UPDATE stack_git_sources SET webhook_secret = ? WHERE stack_id = ?`, sealed, stackID))
}

// load returns the source of one stack, or nil when it has none.
func (g gitStore) load(ctx context.Context, stackID string) (*GitSource, error) {
	if g.box == nil {
		return nil, nil
	}
	row := g.db.QueryRowContext(ctx, `SELECT `+gitColumns+` FROM stack_git_sources WHERE stack_id = ?`, stackID)
	_, source, err := g.scan(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return source, err
}

// loadAll returns the sources of the stacks of an environment (all when empty), by stack id.
func (g gitStore) loadAll(ctx context.Context, environmentID string) (map[string]*GitSource, error) {
	sources := map[string]*GitSource{}
	if g.box == nil {
		return sources, nil
	}
	rows, err := g.db.QueryContext(ctx, `SELECT `+gitColumns+` FROM stack_git_sources
		WHERE stack_id IN (SELECT id FROM stacks WHERE ? = '' OR environment_id = ?)`, environmentID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		id, source, err := g.scan(rows)
		if err != nil {
			return nil, err
		}
		sources[id] = source
	}
	return sources, rows.Err()
}

func (g gitStore) scan(row rowScanner) (string, *GitSource, error) {
	var id, sealed, watch, accepted, pending, deployedAt string
	var source GitSource
	err := row.Scan(&id, &sealed, &source.Branch, &source.Tag, &source.BaseDirectory, &source.ComposePath,
		&source.PrivateKeyID, &watch, &source.AutoDeploy, &accepted, &pending, &source.LastRef, &source.LastCommit,
		&source.LastCommitMessage, &deployedAt)
	if err != nil {
		return "", nil, err
	}
	if source.Repository, err = g.box.Open(sealed); err != nil {
		return "", nil, fmt.Errorf("open repository of stack %s: %w", id, err)
	}
	for target, raw := range map[*[]string]string{&source.WatchPaths: watch, &source.AcceptedRisks: accepted, &source.PendingRisks: pending} {
		if err := json.Unmarshal([]byte(raw), target); err != nil {
			return "", nil, fmt.Errorf("decode git source of stack %s: %w", id, err)
		}
	}
	if deployedAt != "" {
		moment := sqlite.ParseTime(deployedAt)
		source.LastDeployedAt = &moment
	}
	return id, &source, nil
}

func jsonList(values []string) string {
	if values == nil {
		return "[]"
	}
	encoded, _ := json.Marshal(values)
	return string(encoded)
}
