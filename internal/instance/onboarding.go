package instance

import (
	"context"
	"strings"
)

type Onboarding struct {
	HTTPS         bool `json:"https"`
	Backup        bool `json:"backup"`
	Notifications bool `json:"notifications"`
	Deployed      bool `json:"deployed"`
}

func (s *Service) Onboarding(ctx context.Context) (Onboarding, error) {
	var o Onboarding
	var publicURL string
	if err := s.DB.QueryRowContext(ctx, `SELECT COALESCE((SELECT value FROM settings WHERE key='public_url'),'')`).Scan(&publicURL); err != nil {
		return o, err
	}
	o.HTTPS = strings.HasPrefix(publicURL, "https://")
	status, err := s.Status(ctx)
	if err != nil {
		return o, err
	}
	o.Backup = status.LastBackupAt != nil
	if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM notification_channels WHERE enabled=1),EXISTS(SELECT 1 FROM applications WHERE current_image <> '')`).Scan(&o.Notifications, &o.Deployed); err != nil {
		return o, err
	}
	return o, nil
}
