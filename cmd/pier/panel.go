package main

import (
	"context"
	"os"
	"path/filepath"
	"pier/internal/platform/i18n"

	"gopkg.in/yaml.v3"

	"pier/internal/compose"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

// configurePanel writes into the protected file provider so no per-server
// routing capability is copied into a committed file or container label.
func configurePanel(ctx context.Context, servers *server.Service, domain string) error {

	if domain != "" {
		if _, err := compose.ParseRoute("https://" + domain); err != nil {
			return i18n.Errorf("PIER_DOMAIN inválido: %w", err)
		}
		if containsPanelPath(domain) {
			return i18n.Errorf("PIER_DOMAIN deve conter apenas o domínio")
		}
	}
	all, err := servers.List(ctx)
	if err != nil {
		return err
	}
	for _, srv := range all {
		if srv.Kind != "local" {
			continue
		}
		if domain == "" {
			err := os.Remove(filepath.Join(srv.ProxyDir(), "dynamic", "pier-panel.yaml"))
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		data, err := panelConfiguration(domain)
		if err != nil {
			return err
		}
		dir := filepath.Join(srv.ProxyDir(), "dynamic")
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return (&executor.Local{}).WriteFile(ctx, filepath.Join(dir, "pier-panel.yaml"), data, 0600)
	}
	return i18n.Errorf("servidor local não encontrado para PIER_DOMAIN")
}
func containsPanelPath(domain string) bool {
	for _, r := range domain {
		if r == '/' || r == '?' || r == '#' || r == '@' || r == ':' {
			return true
		}
	}
	return false
}
func panelConfiguration(domain string) ([]byte, error) {
	service := map[string]any{"loadBalancer": map[string]any{"servers": []any{map[string]string{"url": "http://pier-panel:8700"}}}}
	router := func(entry string, tls bool) map[string]any {
		r := map[string]any{"rule": "Host(`" + domain + "`)", "entryPoints": []string{entry}, "service": "pier-panel"}
		if tls {
			r["tls"] = map[string]string{"certResolver": "letsencrypt"}
		} else {
			r["middlewares"] = []string{"pier-panel-https"}
		}
		return r
	}
	return yaml.Marshal(map[string]any{"http": map[string]any{"routers": map[string]any{"pier-panel-http": router("http", false), "pier-panel-https": router("https", true)}, "services": map[string]any{"pier-panel": service}, "middlewares": map[string]any{"pier-panel-https": map[string]any{"redirectScheme": map[string]any{"scheme": "https", "permanent": true}}}}})
}
