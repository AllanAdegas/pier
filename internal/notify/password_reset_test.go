package notify

import (
	"context"
	"log/slog"
	"path/filepath"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"strings"
	"testing"
)

type resetCapture struct {
	config map[string]string
	event  Event
}

func (*resetCapture) RequiredConfig() []string { return nil }
func (c *resetCapture) Send(_ context.Context, config map[string]string, e Event) error {
	c.config = config
	c.event = e
	return nil
}
func TestPasswordResetUsesOnlyAccountRecipientAndRequiresTLS(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	box, _ := secretbox.New("reset-mailer-test-key")
	s := NewService(db, box, slog.New(slog.DiscardHandler))
	capture := &resetCapture{}
	s.senders[KindEmail] = capture
	if s.Available(ctx) {
		t.Fatal("unexpected reset channel")
	}
	_, err = s.Create(ctx, Input{Name: "smtp", Kind: KindEmail, Enabled: true, Events: []EventType{DeploymentFailed}, Config: map[string]string{"host": "localhost", "port": "25", "from": "panel@example.com", "to": "admin@example.com"}})
	if err != nil {
		t.Fatal(err)
	}
	if !s.Available(ctx) {
		t.Fatal("missing reset channel")
	}
	if err := s.SendPasswordReset(ctx, "account@example.com", "https://panel.example/#reset=token"); err != nil {
		t.Fatal(err)
	}
	if capture.config["to"] != "account@example.com" || capture.config["require_tls"] != "true" {
		t.Fatal("reset mail exposed to channel recipients or without TLS")
	}
	if !strings.Contains(capture.event.Message, "#reset=token") {
		t.Fatal("missing link")
	}
	channels, _ := s.List(ctx)
	if channels[0].Config["to"] != "admin@example.com" {
		t.Fatal("channel mutated")
	}
}
