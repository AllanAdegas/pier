package gitprovider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"pier/internal/platform/apperr"
)

// MaxWebhookBytes bounds a webhook body (GitHub caps payloads at 25 MB, but
// push and pull request events of real repositories stay far below this).
const MaxWebhookBytes = 2 << 20

// Events of the App webhook that deploy something.
const (
	EventPush        = "push"
	EventPullRequest = "pull_request"
)

// Delivery is an authenticated App webhook. Targets are the resources linked
// to the repository of a push or pull request event; empty otherwise.
type Delivery struct {
	Event     string
	Duplicate bool
	Targets   []Target
}

type webhookPayload struct {
	Action       string `json:"action"`
	Installation struct {
		ID int64 `json:"id"`
	} `json:"installation"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

// Webhook authenticates an App webhook with the App secret before reading
// it, records installation changes and tells which resources a repository
// event concerns. A delivery is processed once, however often GitHub sends it.
func (s *Service) Webhook(ctx context.Context, delivery, event, signature string, body []byte) (Delivery, error) {
	if delivery == "" || len(delivery) > 200 || len(body) > MaxWebhookBytes {
		return Delivery{}, apperr.Invalid("Webhook do GitHub inválido.")
	}
	c, err := s.config(ctx)
	if err != nil {
		return Delivery{}, apperr.Unauthorized("Assinatura do GitHub inválida.")
	}
	if !validSignature(c.WebhookSecret, signature, body) {
		return Delivery{}, apperr.Unauthorized("Assinatura do GitHub inválida.")
	}
	result := Delivery{Event: event}
	if !handledEvent(event) {
		return result, nil
	}
	var payload webhookPayload
	if err := json.Unmarshal(body, &payload); err != nil || event != "ping" && payload.Installation.ID <= 0 {
		return Delivery{}, apperr.Invalid("Webhook do GitHub inválido.")
	}
	first, err := s.recordDelivery(ctx, delivery, event, payload)
	if err != nil || !first {
		return Delivery{Event: event, Duplicate: !first}, err
	}
	if event == EventPush || event == EventPullRequest {
		result.Targets, err = s.targets(ctx, payload.Installation.ID, payload.Repository.FullName)
	}
	return result, err
}

func handledEvent(event string) bool {
	switch event {
	case "ping", "installation", "installation_repositories", EventPush, EventPullRequest:
		return true
	}
	return false
}

func validSignature(secret, signature string, body []byte) bool {
	hexValue, found := strings.CutPrefix(signature, "sha256=")
	provided, err := hex.DecodeString(hexValue)
	if !found || err != nil || secret == "" || len(provided) != sha256.Size {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}

// recordDelivery deduplicates the delivery and applies installation
// changes in one transaction. It reports whether this is the first time.
func (s *Service) recordDelivery(ctx context.Context, delivery, event string, payload webhookPayload) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO git_provider_deliveries(delivery) VALUES(?)`, delivery)
	if err != nil {
		return false, err
	}
	if n, _ := inserted.RowsAffected(); n == 0 {
		return false, nil
	}
	if event == "installation" {
		suspended := 0
		if payload.Action == "deleted" || payload.Action == "suspend" {
			suspended = 1
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO git_provider_installations(id,suspended) VALUES(?,?)
			ON CONFLICT(id) DO UPDATE SET suspended=excluded.suspended`, payload.Installation.ID, suspended)
		if err != nil {
			return false, err
		}
	}
	// A bounded replay window; installation mutations themselves are upserts.
	if _, err = tx.ExecContext(ctx, `DELETE FROM git_provider_deliveries WHERE received_at < datetime('now','-30 days')`); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	if event == "installation" || event == "installation_repositories" {
		s.forgetInstallation(payload.Installation.ID)
	}
	return true, nil
}
