package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/sqlite"
)

type UptimeCheck struct {
	ID              string `json:"id"`
	Resource        string `json:"resource"`
	URL             string `json:"url"`
	ExpectedStatus  int    `json:"expected_status"`
	IntervalSeconds int    `json:"interval_seconds"`
	Enabled         bool   `json:"enabled"`
	LastChecked     string `json:"last_checked"`
}
type UptimeResult struct {
	CheckedAt string `json:"checked_at"`
	Status    int    `json:"status"`
	Healthy   bool   `json:"healthy"`
}

func (s *State) SaveUptime(ctx context.Context, c UptimeCheck) (UptimeCheck, error) {
	u, err := url.Parse(c.URL)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || c.Resource == "" || c.IntervalSeconds < 60 || c.IntervalSeconds > 86400 || c.ExpectedStatus < 100 || c.ExpectedStatus > 599 {
		return c, apperr.Invalid("Informe URL http(s), recurso, status esperado e intervalo entre 60 e 86400 segundos.")
	}
	if c.ID == "" {
		c.ID = idgen.New()
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO uptime_checks(id,resource,url,expected_status,interval_seconds,enabled) VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET resource=excluded.resource,url=excluded.url,expected_status=excluded.expected_status,interval_seconds=excluded.interval_seconds,enabled=excluded.enabled,last_checked=''`, c.ID, c.Resource, c.URL, c.ExpectedStatus, c.IntervalSeconds, c.Enabled)
	return c, err
}
func (s *State) DeleteUptime(ctx context.Context, id string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM uptime_checks WHERE id=?`, id)
	return err
}
func (s *State) UptimeChecks(ctx context.Context) ([]UptimeCheck, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,resource,url,expected_status,interval_seconds,enabled,last_checked FROM uptime_checks ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	checks := []UptimeCheck{}
	for rows.Next() {
		var c UptimeCheck
		if err := rows.Scan(&c.ID, &c.Resource, &c.URL, &c.ExpectedStatus, &c.IntervalSeconds, &c.Enabled, &c.LastChecked); err != nil {
			return nil, err
		}
		checks = append(checks, c)
	}
	return checks, rows.Err()
}
func (s *State) UptimeHistory(ctx context.Context, id string) ([]UptimeResult, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT checked_at,status,healthy FROM uptime_history WHERE check_id=? AND checked_at>=? ORDER BY checked_at DESC`, id, sqlite.FormatTime(time.Now().Add(-7*24*time.Hour)))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	history := []UptimeResult{}
	for rows.Next() {
		var r UptimeResult
		if err := rows.Scan(&r.CheckedAt, &r.Status, &r.Healthy); err != nil {
			return nil, err
		}
		history = append(history, r)
	}
	return history, rows.Err()
}
func (m *Monitor) checkUptime(ctx context.Context) {
	if m.Alerts == nil {
		return
	}
	checks, err := m.Alerts.UptimeChecks(ctx)
	if err != nil {
		return
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	for _, c := range checks {
		if !c.Enabled || time.Since(sqlite.ParseTime(c.LastChecked)) < time.Duration(c.IntervalSeconds)*time.Second {
			continue
		}
		status := 0
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		if err == nil {
			response, e := client.Do(req)
			if e == nil {
				status = response.StatusCode
				response.Body.Close()
			}
		}
		now := sqlite.FormatTime(time.Now())
		healthy := status == c.ExpectedStatus
		tx, err := m.Alerts.DB.BeginTx(ctx, nil)
		if err != nil {
			continue
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO uptime_history(check_id,checked_at,status,healthy) VALUES(?,?,?,?)`, c.ID, now, status, healthy)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE uptime_checks SET last_checked=? WHERE id=?`, now, c.ID)
		}
		if err != nil {
			tx.Rollback()
			continue
		}
		if tx.Commit() != nil {
			continue
		}
		m.observe(ctx, notify.UptimeDown, c.Resource+":"+c.ID, i18n.Instance().Text("A URL não respondeu com o status esperado. Confira o proxy, o DNS e os logs da aplicação."), !healthy)
	}
	_, _ = m.Alerts.DB.ExecContext(ctx, `DELETE FROM uptime_history WHERE checked_at<?`, sqlite.FormatTime(time.Now().Add(-7*24*time.Hour)))
}

// certificateIssue uses normal trust and hostname verification, including SNI.
func certificateIssue(ctx context.Context, raw string, now time.Time) (bool, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" {
		return false, nil
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 5 * time.Second}, Config: &tls.Config{ServerName: u.Hostname(), MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
	if err != nil {
		return true, err
	}
	defer conn.Close()
	certs := conn.(*tls.Conn).ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return true, fmt.Errorf("no peer certificate")
	}
	return certs[0].NotAfter.Before(now.Add(14 * 24 * time.Hour)), nil
}
func (m *Monitor) checkCertificates(ctx context.Context) {
	if m.Alerts == nil {
		return
	}
	routes := map[string][]string{}
	if apps, err := m.Applications.List(ctx, ""); err == nil {
		for _, app := range apps {
			if app.CurrentImage != "" {
				routes[app.ID] = app.Config.Network.Domains
			}
		}
	}
	if stacks, err := m.Stacks.List(ctx, ""); err == nil {
		for _, s := range stacks {
			for _, r := range s.Routes {
				routes[s.ID] = append(routes[s.ID], r.Domain)
			}
		}
	}
	for resource, domains := range routes {
		for _, domain := range domains {
			u, err := url.Parse(domain)
			if err != nil || u.Scheme != "https" {
				continue
			}
			bad, _ := certificateIssue(ctx, domain, time.Now())
			m.observe(ctx, notify.CertificateExpiring, resource+":"+domain, i18n.Instance().Text("O certificado HTTPS é inválido ou vence em menos de 14 dias. Confira o DNS, a renovação e os logs do proxy."), bad)
		}
	}
}
