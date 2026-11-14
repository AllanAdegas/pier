package secretbox

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// UpgradeReport counts what UpgradeLegacy did. It never carries values.
type UpgradeReport struct {
	// AlreadyDone is true when a previous run had finished: nothing was read.
	AlreadyDone bool
	// Ciphertexts is the number of v1 ciphertexts rewritten as v2.
	Ciphertexts int
	// Rows is the number of rows updated (a JSON value may hold several).
	Rows int64
	// Fields is the number of registered fields that held v1 data.
	Fields int
}

// UpgradeLegacy rewrites as "v2:" the "v1:" ciphertexts stored in the given
// fields, and only there, in a single transaction. A whole-value field is
// upgraded when its value starts with "v1:"; a JSON field only at its
// declared paths. Text in any other table, column, row or JSON key is never
// read or changed, even when it contains a copied ciphertext.
//
// If a value at a registered location starts with "v1:" but does not open
// (wrong key, tampering, damage), the transaction rolls back and the stored
// data stays exactly as it was. Once it succeeds,
// secretbox_state.legacy_upgraded_at is set in the same transaction, so later
// starts skip the work; a restored pre-0.8 database has no marker and is
// upgraded on its first start. Running it again is always safe.
func UpgradeLegacy(ctx context.Context, db *sql.DB, box *Box, fields []Field) (UpgradeReport, error) {
	if err := validateFields(fields); err != nil {
		return UpgradeReport{}, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return UpgradeReport{}, fmt.Errorf("secretbox upgrade: begin: %w", err)
	}
	defer tx.Rollback()
	var done sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT legacy_upgraded_at FROM secretbox_state WHERE id = 1`).Scan(&done); err != nil {
		return UpgradeReport{}, fmt.Errorf("secretbox upgrade: read state (salt not created?): %w", err)
	}
	if done.Valid {
		return UpgradeReport{AlreadyDone: true}, nil
	}
	report, err := upgradeFields(ctx, tx, box, fields)
	if err != nil {
		return UpgradeReport{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE secretbox_state SET legacy_upgraded_at = ? WHERE id = 1`,
		time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return UpgradeReport{}, fmt.Errorf("secretbox upgrade: mark done: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return UpgradeReport{}, fmt.Errorf("secretbox upgrade: commit: %w", err)
	}
	return report, nil
}

func upgradeFields(ctx context.Context, tx *sql.Tx, box *Box, fields []Field) (UpgradeReport, error) {
	var report UpgradeReport
	for _, field := range fields {
		if err := upgradeField(ctx, tx, box, field, &report); err != nil {
			return UpgradeReport{}, err
		}
	}
	return report, nil
}

func upgradeField(ctx context.Context, tx *sql.Tx, box *Box, field Field, report *UpgradeReport) error {
	query, args := field.candidatesQuery()
	values, err := queryStrings(ctx, tx, query, args...)
	if err != nil {
		return fmt.Errorf("secretbox upgrade: scan %s: %w", field, err)
	}
	changed := false
	for _, value := range values {
		upgraded, count, err := box.resealField(field, value)
		if err != nil {
			return fmt.Errorf("secretbox upgrade: %s: %w", field, err)
		}
		if count == 0 {
			continue
		}
		query, args := field.replaceStatement(value, upgraded)
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("secretbox upgrade: update %s: %w", field, err)
		}
		rows, _ := result.RowsAffected()
		report.Rows += rows
		report.Ciphertexts += count
		changed = true
	}
	if changed {
		report.Fields++
	}
	return nil
}

// resealField returns value with its legacy ciphertexts rewritten, and how
// many it rewrote.
func (b *Box) resealField(field Field, value string) (string, int, error) {
	if !field.wholeValue() {
		return b.resealJSONPaths(value, field.JSONPaths)
	}
	if !IsLegacy(value) {
		return value, 0, nil
	}
	upgraded, err := b.reseal(value)
	if err != nil {
		return "", 0, err
	}
	return upgraded, 1, nil
}

// candidatesQuery selects the distinct text values of field that may hold a
// legacy ciphertext. COLLATE BINARY and typeof keep column collations and
// non-text storage classes from widening the match.
func (f Field) candidatesQuery() (string, []any) {
	column := quoteIdentifier(f.Column)
	match := `instr(` + column + `, 'v1:') > 0`
	if f.wholeValue() {
		match = `substr(` + column + `, 1, 3) COLLATE BINARY = 'v1:'`
	}
	filter, args := f.rowFilter()
	return `SELECT DISTINCT ` + column + ` COLLATE BINARY FROM ` + quoteIdentifier(f.Table) +
		` WHERE typeof(` + column + `) = 'text' AND ` + match + filter, args
}

// replaceStatement updates exactly the rows of field holding old.
func (f Field) replaceStatement(old, upgraded string) (string, []any) {
	column := quoteIdentifier(f.Column)
	filter, filterArgs := f.rowFilter()
	query := `UPDATE ` + quoteIdentifier(f.Table) + ` SET ` + column + ` = ? WHERE typeof(` + column +
		`) = 'text' AND ` + column + ` COLLATE BINARY = ?` + filter
	return query, append([]any{upgraded, old}, filterArgs...)
}

func (f Field) rowFilter() (string, []any) {
	if f.Row.Column == "" {
		return "", nil
	}
	return ` AND ` + quoteIdentifier(f.Row.Column) + ` COLLATE BINARY = ?`, []any{f.Row.Value}
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func queryStrings(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}
