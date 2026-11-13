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
		// The reset is asked from the sign-in page, so the e-mail speaks the
		// language of that page (FromContext falls back to the instance's).
		language := i18n.FromContext(ctx)
		return s.senders[KindEmail].Send(ctx, config, Event{Title: language.Text("Pier — redefinir senha"), Message: language.Sprintf("Para redefinir sua senha, abra o link em até 30 minutos:\n%s\nSe você não pediu, ignore esta mensagem.", link)})
	}
	return errors.New("no enabled email channel")
}
