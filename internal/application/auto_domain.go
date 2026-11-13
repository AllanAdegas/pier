package application

import (
	"context"
	"database/sql"
	"net"
	"net/netip"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

// AutoDomain is the domain Pier generated for an application. Removed means
// the user took it out of the configuration: it is never added back unless
// the user asks for a new one.
type AutoDomain struct {
	ApplicationID string    `json:"application_id"`
	Domain        string    `json:"domain"`
	URL           string    `json:"url"`
	Automatic     bool      `json:"automatic"`
	Removed       bool      `json:"removed"`
	CreatedAt     time.Time `json:"created_at"`
}

const maxDomainLabel = 63

// autoDomainLabel is "<app>-<env>-<short>": readable, DNS-safe, ≤ 63 chars.
func autoDomainLabel(appName, environmentName, short string) string {
	suffix := "-" + dnsSafe(environmentName) + "-" + short
	if environmentName == "" || dnsSafe(environmentName) == "" {
		suffix = "-" + short
	}
	name := dnsSafe(appName)
	if name == "" {
		name = "app"
	}
	if room := maxDomainLabel - len(suffix); len(name) > room {
		name = strings.TrimRight(name[:max(room, 1)], "-")
	}
	return name + suffix
}

func dnsSafe(value string) string {
	var builder strings.Builder
	dash := false
	for _, char := range strings.ToLower(value) {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') {
			builder.WriteRune(char)
			dash = false
		} else if !dash && builder.Len() > 0 {
			builder.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// autoDomainHost joins the label with the base domain, or with
// <ipv4>.sslip.io when no base domain is configured.
func autoDomainHost(label, baseDomain string, address netip.Addr) (string, error) {
	if baseDomain != "" {
		return label + "." + baseDomain, nil
	}
	if !address.Is4() {
		return "", i18n.Errorf("o domínio automático com sslip.io precisa do IPv4 público do servidor; configure um domínio base")
	}
	return label + "." + address.String() + "." + sslipBase, nil
}

// autoDomainCandidates are tried in order until one is free: the short id
// grows, then a random suffix is added.
func autoDomainCandidates(app Application, environmentName string) []string {
	id := dnsSafe(app.ID)
	candidates := []string{}
	for _, size := range []int{6, 10} {
		candidates = append(candidates, autoDomainLabel(app.Name, environmentName, id[:min(size, len(id))]))
	}
	for range 3 {
		candidates = append(candidates, autoDomainLabel(app.Name, environmentName, id[:min(6, len(id))]+idgen.Token(2)))
	}
	return candidates
}

// pickAutoDomain returns the first candidate host not used by any application
// nor reserved by another automatic domain.
func (s *Store) pickAutoDomain(ctx context.Context, app Application, environmentName string, settings AutoDomainSettings, address netip.Addr) (string, error) {
	taken, err := s.domainsInUse(ctx, app.ID)
	if err != nil {
		return "", err
	}
	for _, label := range autoDomainCandidates(app, environmentName) {
		host, err := autoDomainHost(label, settings.BaseDomain, address)
		if err != nil {
			return "", err
		}
		if !taken[host] {
			return host, nil
		}
	}
	return "", apperr.Conflict("Não foi possível gerar um domínio automático livre. Informe um domínio.")
}

func (s *Store) domainsInUse(ctx context.Context, exceptApplicationID string) (map[string]bool, error) {
	taken := map[string]bool{}
	apps, err := s.List(ctx, "")
	if err != nil {
		return nil, err
	}
	for _, other := range apps {
		if other.ID == exceptApplicationID {
			continue
		}
		for _, domain := range other.Config.Network.Domains {
			if route, err := compose.ParseRoute(domain); err == nil {
				taken[route.Host] = true
			}
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT domain FROM application_auto_domains WHERE application_id <> ?`, exceptApplicationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return nil, err
		}
		taken[domain] = true
	}
	return taken, rows.Err()
}

func autoDomainURL(host string, settings AutoDomainSettings) string {
	if settings.HTTPS {
		return "https://" + host
	}
	return "http://" + host
}

func (s *Store) autoDomain(ctx context.Context, applicationID string) (AutoDomain, error) {
	record := AutoDomain{ApplicationID: applicationID}
	var createdAt string
	err := s.db.QueryRowContext(ctx, `SELECT domain, removed, created_at FROM application_auto_domains WHERE application_id = ?`,
		applicationID).Scan(&record.Domain, &record.Removed, &createdAt)
	if err == sql.ErrNoRows {
		return AutoDomain{}, sqlite.ErrNotFound
	}
	record.CreatedAt = sqlite.ParseTime(createdAt)
	return record, err
}

func (s *Store) saveAutoDomain(ctx context.Context, applicationID, domain string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO application_auto_domains (application_id, domain, removed, created_at)
		VALUES (?, ?, 0, ?) ON CONFLICT (application_id) DO UPDATE SET domain = excluded.domain, removed = 0,
		created_at = excluded.created_at`, applicationID, domain, sqlite.FormatTime(time.Now()))
	return err
}

func (s *Store) markAutoDomainRemoved(ctx context.Context, applicationID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE application_auto_domains SET removed = 1 WHERE application_id = ?`, applicationID)
	return err
}

func (s *Store) environmentName(ctx context.Context, environmentID string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM environments WHERE id = ?`, environmentID).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// configHasHost reports whether one of the configured domains uses host.
func configHasHost(config Config, host string) bool {
	for _, domain := range config.Network.Domains {
		if route, err := compose.ParseRoute(domain); err == nil && route.Host == host {
			return true
		}
	}
	return false
}

// servesWeb reports whether the application has a web process that can
// receive a domain (host networking publishes nothing through the proxy).
func servesWeb(config Config) bool {
	if config.DockerOptions.NetworkMode == hostMode {
		return false
	}
	if len(config.Processes) == 0 {
		return true
	}
	for _, process := range config.Processes {
		if process.Name == WebProcess {
			return true
		}
	}
	return false
}

// serverPublicIPv4 finds the address sslip.io names point to: the server's
// host when it is (or resolves to) an IPv4, else what the server reports.
func serverPublicIPv4(ctx context.Context, target server.Server, ex executor.Executor) (netip.Addr, error) {
	host := strings.TrimSpace(target.Host)
	if address, err := netip.ParseAddr(host); err == nil {
		return address.Unmap(), nil
	}
	if host != "" {
		addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		if err == nil && len(addresses) > 0 {
			return addresses[0].Unmap(), nil
		}
	}
	output, err := ex.Run(ctx, "curl -4 --fail --silent --show-error --connect-timeout 2 --max-time 5 https://api.ipify.org")
	if err != nil {
		return netip.Addr{}, i18n.Errorf("não foi possível descobrir o IP público do servidor para o domínio automático: %w", err)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(output))
	if err != nil {
		return netip.Addr{}, i18n.Errorf("o servidor não informou um IP público válido para o domínio automático")
	}
	return address.Unmap(), nil
}
