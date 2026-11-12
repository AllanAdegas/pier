package notify

import (
	"context"
	"errors"
	"maps"
	"pier/internal/platform/i18n"
)

// Available reports whether there is an enabled SMTP channel for account mail.
func (s *Service) Available(ctx context.Context) bool {
	channels, err := s.List(ctx)
	if err != nil {
		return false
	}
	for _, c := range channels {
		if c.Enabled && c.Kind == KindEmail {
			return true
		}
	}
	return false
}

// SendPasswordReset reuses SMTP credentials, overriding recipients with the account
// owner. Reset tokens never enter the normal notification fanout or event log.
func (s *Service) SendPasswordReset(ctx context.Context, to, link string) error {
	channels, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, c := range channels {
		if !c.Enabled || c.Kind != KindEmail {
			continue
		}
		config := maps.Clone(c.Config)
		config["to"] = to
		config["require_tls"] = "true"
		return s.senders[KindEmail].Send(ctx, config, Event{Title: i18n.Instance().Text("Pier — redefinir senha"), Message: i18n.Instance().Sprintf("Para redefinir sua senha, abra o link em até 30 minutos:\n%s\nSe você não pediu, ignore esta mensagem.", link)})
	}
	return errors.New("no enabled email channel")
}
