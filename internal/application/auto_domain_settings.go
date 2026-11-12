package application

import (
	"context"
	"regexp"
	"strings"

	"pier/internal/platform/apperr"
)

// AutoDomainSettings decide whether applications without a domain get one
// automatically, and under which base domain.
type AutoDomainSettings struct {
	Enabled bool `json:"enabled"`
	// BaseDomain is a wildcard domain whose DNS (*.<base>) points to the
	// servers. Empty uses <server-ipv4>.sslip.io, which needs no DNS setup.
	BaseDomain string `json:"base_domain"`
	// HTTPS requests certificates. Only for a custom base domain the ACME
	// certificate authority can validate; sslip.io domains are always HTTP.
	HTTPS bool `json:"https"`
}

const (
	settingAutoDomainEnabled = "engine_auto_domain_enabled"
	settingAutoDomainBase    = "engine_auto_domain_base"
	settingAutoDomainHTTPS   = "engine_auto_domain_https"
	sslipBase                = "sslip.io"
)

var baseDomainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,63}$`)

// AutoDomainSettings returns the instance's automatic domain settings.
func (s *Service) AutoDomainSettings(ctx context.Context) (AutoDomainSettings, error) {
	return s.store.autoDomainSettings(ctx)
}

// SetAutoDomainSettings validates and saves the automatic domain settings.
func (s *Service) SetAutoDomainSettings(ctx context.Context, next AutoDomainSettings) (AutoDomainSettings, error) {
	next.BaseDomain = strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(next.BaseDomain), "*."), "."))
	fields := apperr.Fields{}
	if next.BaseDomain != "" && (len(next.BaseDomain) > 200 || !baseDomainPattern.MatchString(next.BaseDomain)) {
		fields.Add("base_domain", "Informe um domínio base válido, ex.: apps.exemplo.com (sem *.).")
	}
	if next.HTTPS && next.BaseDomain == "" {
		fields.Add("https", "HTTPS exige um domínio base próprio com DNS curinga; domínios sslip.io usam HTTP.")
	}
	if err := fields.Err(); err != nil {
		return AutoDomainSettings{}, err
	}
	err := s.store.putSettings(ctx, map[string]string{
		settingAutoDomainEnabled: formatBool(next.Enabled),
		settingAutoDomainBase:    next.BaseDomain,
		settingAutoDomainHTTPS:   formatBool(next.HTTPS),
	})
	return next, err
}

func (s *Store) autoDomainSettings(ctx context.Context) (AutoDomainSettings, error) {
	values, err := s.settings(ctx, settingAutoDomainEnabled, settingAutoDomainBase, settingAutoDomainHTTPS)
	if err != nil {
		return AutoDomainSettings{}, err
	}
	return AutoDomainSettings{
		Enabled:    values[settingAutoDomainEnabled] == "true",
		BaseDomain: values[settingAutoDomainBase],
		HTTPS:      values[settingAutoDomainHTTPS] == "true" && values[settingAutoDomainBase] != "",
	}, nil
}
