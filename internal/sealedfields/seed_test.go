package sealedfields

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

type columnInfo struct {
	name, kind          string
	required, isPrimary bool
}

func columnInfos(t *testing.T, db *sql.DB, table string) []columnInfo {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull" AND dflt_value IS NULL, pk > 0 FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []columnInfo
	for rows.Next() {
		var info columnInfo
		if err := rows.Scan(&info.name, &info.kind, &info.required, &info.isPrimary); err != nil {
			t.Fatal(err)
		}
		out = append(out, info)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func tableColumns(t *testing.T, db *sql.DB, table string) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, info := range columnInfos(t, db, table) {
		names[info.name] = true
	}
	return names
}

// insertRow adds a row to table with the given values, filling every other
// required or key column with a placeholder that satisfies the schema's CHECKs.
func insertRow(t *testing.T, db *sql.DB, table string, values map[string]any, sequence int) {
	t.Helper()
	var names []string
	var args []any
	for name, value := range values {
		names, args = append(names, fmt.Sprintf("%q", name)), append(args, value)
	}
	for _, info := range columnInfos(t, db, table) {
		_, given := values[info.name]
		_, overridden := placeholderOverrides[table+"."+info.name]
		if given || !(info.required || info.isPrimary || overridden) {
			continue
		}
		names = append(names, fmt.Sprintf("%q", info.name))
		args = append(args, placeholder(table, info.name, info.kind, sequence))
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	query := fmt.Sprintf(`INSERT INTO %q (%s) VALUES (%s)`, table, strings.Join(names, ","), marks)
	if _, err := db.Exec(query, args...); err != nil {
		t.Fatalf("seed %s: %v", table, err)
	}
}

// placeholderOverrides satisfies the CHECK constraints of the seeded tables;
// an overridden column is filled even when it is nullable.
var placeholderOverrides = map[string]any{
	"git_provider_feedback.kind":     "application",
	"git_provider_feedback.provider": "gitlab",
	"git_connections.provider":       "gitlab",
	"access_oidc_config.id":          1,
	"git_provider_config.id":         1,
	"cloud_credentials.provider":     "hetzner",
	"clusters.source":                "imported",
	"env_vars.owner_type":            "application",
	"log_drains.kind":                "http",
	"log_drains.scope_type":          "project",
	"shared_variables.project_id":    "project-1", // exactly one scope column is set
}

func placeholder(table, column, kind string, sequence int) any {
	if value, ok := placeholderOverrides[table+"."+column]; ok {
		return value
	}
	switch strings.ToUpper(kind) {
	case "INTEGER", "REAL":
		return sequence
	case "BLOB":
		return []byte{byte(sequence)}
	default:
		return fmt.Sprintf("placeholder-%s-%d", column, sequence)
	}
}
