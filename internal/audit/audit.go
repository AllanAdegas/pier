// Package audit persists request metadata without credentials or raw bodies.
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"pier/internal/platform/sqlite"
)

type Entry struct {
	ID         int64  `json:"id"`
	OccurredAt string `json:"occurred_at"`
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	TokenID    string `json:"token_id"`
	TokenName  string `json:"token_name"`
	IP         string `json:"ip"`
	Method     string `json:"method"`
	Route      string `json:"route"`
	ResourceID string `json:"resource_id"`
	Status     int    `json:"status"`
	Summary    string `json:"summary"`
}
type Store struct{ DB *sql.DB }
type Filter struct {
	UserID, TokenID, Method, Route, ResourceID, Since, Until string
	Before                                                   int64
	Limit                                                    int
}

func (s *Store) Record(ctx context.Context, e Entry) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_log(occurred_at,user_id,user_name,token_id,token_name,ip,method,route,resource_id,status,summary) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, e.OccurredAt, e.UserID, e.UserName, e.TokenID, e.TokenName, e.IP, e.Method, e.Route, e.ResourceID, e.Status, e.Summary)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM audit_log WHERE occurred_at < ?`, sqlite.FormatTime(time.Now().Add(-90*24*time.Hour)))
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Store) List(ctx context.Context, f Filter) ([]Entry, error) {
	query := `SELECT id,occurred_at,user_id,user_name,token_id,token_name,ip,method,route,resource_id,status,summary FROM audit_log WHERE 1=1`
	args := []any{}
	for _, v := range []struct{ column, value string }{{"user_id", f.UserID}, {"token_id", f.TokenID}, {"method", f.Method}, {"route", f.Route}, {"resource_id", f.ResourceID}} {
		if v.value != "" {
			query += " AND " + v.column + "=?"
			args = append(args, v.value)
		}
	}
	if f.Since != "" {
		query += " AND occurred_at>=?"
		args = append(args, f.Since)
	}
	if f.Until != "" {
		query += " AND occurred_at<=?"
		args = append(args, f.Until)
	}
	if f.Before > 0 {
		query += " AND id<?"
		args = append(args, f.Before)
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 100
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, f.Limit)
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := []Entry{}
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.OccurredAt, &e.UserID, &e.UserName, &e.TokenID, &e.TokenName, &e.IP, &e.Method, &e.Route, &e.ResourceID, &e.Status, &e.Summary); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// Summary discards malformed/truncated bodies and never persists raw input.
func Summary(body []byte, env bool) string {
	if len(body) == 0 {
		return ""
	}
	var value any
	if json.Unmarshal(body, &value) != nil {
		return "[omitted]"
	}
	var mask func(any) any
	mask = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			for k, v := range x {
				lower := strings.ToLower(k)
				sensitive := lower == "current" || lower == "next" || lower == "setup_code" || env && lower != "key" && lower != "name" && lower != "build_time" && lower != "secret_build"
				for _, part := range []string{"secret", "password", "passphrase", "token", "key", "value", "credential", "kubeconfig", "compose", "command", "script", "output", "repository", "url", "endpoint", "authorization", "header", "env", "data", "content", "manifest"} {
					sensitive = sensitive || strings.Contains(lower, part)
				}
				if sensitive {
					x[k] = "[redacted]"
				} else {
					x[k] = mask(v)
				}
			}
		case []any:
			for i, v := range x {
				x[i] = mask(v)
			}
		}
		return v
	}
	encoded, err := json.Marshal(mask(value))
	if err != nil {
		return "[omitted]"
	}
	if len(encoded) > 8192 {
		return "[omitted]"
	}
	return string(encoded)
}
