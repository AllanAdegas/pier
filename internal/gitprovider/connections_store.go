package gitprovider

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// connectionRecord is a stored connection with its sealed token.
type connectionRecord struct {
	Connection
	sealedToken string
}

const connectionColumns = `id,provider,name,base_url,workspace,encrypted_token,account,scopes,created_at,updated_at`

type rowScanner interface{ Scan(...any) error }

func scanConnection(row rowScanner) (connectionRecord, error) {
	var record connectionRecord
	var scopes string
	err := row.Scan(&record.ID, &record.Provider, &record.Name, &record.BaseURL, &record.Workspace,
		&record.sealedToken, &record.Account, &scopes, &record.CreatedAt, &record.UpdatedAt)
	record.Scopes = splitScopes(scopes)
	record.TokenSet = record.sealedToken != ""
	return record, err
}

func splitScopes(value string) []string {
	scopes := []string{}
	for _, scope := range strings.Split(value, ",") {
		if scope = strings.TrimSpace(scope); scope != "" {
			scopes = append(scopes, scope)
		}
	}
	return scopes
}

func (c *Connections) record(ctx context.Context, id string) (connectionRecord, error) {
	record, err := scanConnection(c.service.db.QueryRowContext(ctx,
		`SELECT `+connectionColumns+` FROM git_connections WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return record, connectionNotFound()
	}
	return record, err
}

func (c *Connections) insert(ctx context.Context, record connectionRecord) error {
	_, err := c.service.db.ExecContext(ctx, `INSERT INTO git_connections(`+connectionColumns+`) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		record.ID, record.Provider, record.Name, record.BaseURL, record.Workspace, record.sealedToken,
		record.Account, strings.Join(record.Scopes, ","), record.CreatedAt, record.UpdatedAt)
	return err
}

func (c *Connections) update(ctx context.Context, record connectionRecord) error {
	_, err := c.service.db.ExecContext(ctx, `UPDATE git_connections SET name=?,encrypted_token=?,account=?,scopes=?,updated_at=?
		WHERE id=?`, record.Name, record.sealedToken, record.Account, strings.Join(record.Scopes, ","), record.UpdatedAt, record.ID)
	return err
}

// List returns every connection, without tokens.
func (c *Connections) List(ctx context.Context) ([]Connection, error) {
	rows, err := c.service.db.QueryContext(ctx, `SELECT `+connectionColumns+` FROM git_connections ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Connection{}
	for rows.Next() {
		record, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, record.Connection)
	}
	return result, rows.Err()
}

// Get returns one connection, without its token.
func (c *Connections) Get(ctx context.Context, id string) (Connection, error) {
	record, err := c.record(ctx, id)
	return record.Connection, err
}

func (c *Connections) linkCount(ctx context.Context, id string) (int, error) {
	var count int
	err := c.service.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM git_connection_links WHERE connection_id=? AND orphaned=0`, id).Scan(&count)
	return count, err
}
