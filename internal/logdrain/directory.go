package logdrain

import (
	"context"
	"database/sql"
)

// resource is one application or stack whose containers a drain forwards,
// with the servers where they run.
type resource struct {
	Type, ID string
	Servers  []string
}

func (r resource) key() string { return r.Type + "/" + r.ID }

// directory resolves a drain scope from Pier's tables. Previews of an
// application in a project are part of the project; extra servers of a
// multi-server application come from placement_status when it exists.
type directory struct{ db *sql.DB }

var scopeQueries = map[string]string{
	ScopeProject: `
		SELECT 'application', a.id, a.server_id FROM applications a
			JOIN environments e ON e.id = a.environment_id WHERE e.project_id = ?1
		UNION SELECT 'application', p.id, a.server_id FROM application_previews p
			JOIN applications a ON a.id = p.application_id
			JOIN environments e ON e.id = a.environment_id WHERE e.project_id = ?1
		UNION SELECT 'stack', s.id, s.server_id FROM stacks s
			JOIN environments e ON e.id = s.environment_id WHERE e.project_id = ?1`,
	ScopeApplication: `SELECT 'application', id, server_id FROM applications WHERE id = ?1`,
	ScopeStack:       `SELECT 'stack', id, server_id FROM stacks WHERE id = ?1`,
}

var existsQueries = map[string]string{
	ScopeProject:     `SELECT EXISTS(SELECT 1 FROM projects WHERE id = ?)`,
	ScopeApplication: `SELECT EXISTS(SELECT 1 FROM applications WHERE id = ?)`,
	ScopeStack:       `SELECT EXISTS(SELECT 1 FROM stacks WHERE id = ?)`,
}

func (d directory) exists(ctx context.Context, scopeType, id string) (bool, error) {
	query, known := existsQueries[scopeType]
	if !known {
		return false, nil
	}
	var found bool
	err := d.db.QueryRowContext(ctx, query, id).Scan(&found)
	return found, err
}

func (d directory) resources(ctx context.Context, scopeType, id string) ([]resource, error) {
	query, known := scopeQueries[scopeType]
	if !known {
		return []resource{}, nil
	}
	rows, err := d.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byKey := map[string]int{}
	list := []resource{}
	for rows.Next() {
		var item resource
		var serverID string
		if err := rows.Scan(&item.Type, &item.ID, &serverID); err != nil {
			return nil, err
		}
		if index, found := byKey[item.key()]; found {
			list[index].Servers = append(list[index].Servers, serverID)
			continue
		}
		item.Servers = []string{serverID}
		byKey[item.key()] = len(list)
		list = append(list, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range list {
		list[index].Servers = append(list[index].Servers, d.placements(ctx, list[index])...)
	}
	return list, nil
}

// placements is best effort: without the multi-server table (or on any
// error) the primary server alone is used.
func (d directory) placements(ctx context.Context, item resource) []string {
	if item.Type != ScopeApplication {
		return nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT server_id FROM placement_status WHERE application_id = ?`, item.ID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	servers := []string{}
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			servers = append(servers, id)
		}
	}
	return servers
}
