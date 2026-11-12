package gitprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"pier/internal/platform/apperr"
	"strings"
)

func (s *Service) Webhook(ctx context.Context, delivery, event, signature string, body []byte) error {
	if delivery == "" || len(delivery) > 200 || len(body) > 2<<20 {
		return invalid()
	}
	c, err := s.config(ctx)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(c.WebhookSecret))
	mac.Write(body)
	provided, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || !strings.HasPrefix(signature, "sha256=") || !hmac.Equal(provided, mac.Sum(nil)) {
		return apperr.Unauthorized("Assinatura do GitHub inválida.")
	}
	if event != "installation" && event != "installation_repositories" && event != "ping" {
		return nil
	}
	var input struct {
		Action       string `json:"action"`
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err = json.Unmarshal(body, &input); err != nil {
		return invalid()
	}
	if event != "ping" && input.Installation.ID <= 0 {
		return invalid()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO git_provider_deliveries(delivery) VALUES(?)`, delivery)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return nil
	}
	if event == "installation" {
		suspended := 0
		if input.Action == "deleted" || input.Action == "suspend" {
			suspended = 1
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO git_provider_installations(id,suspended) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET suspended=excluded.suspended`, input.Installation.ID, suspended)
		if err != nil {
			return err
		}
	}
	// Retain a bounded replay window; installation mutations themselves are upserts.
	if _, err = tx.ExecContext(ctx, `DELETE FROM git_provider_deliveries WHERE received_at < datetime('now','-30 days')`); err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		delete(s.tokens, input.Installation.ID)
		s.scopedTokens = map[string]cachedToken{}
	}
	return err
}
