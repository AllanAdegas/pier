package application

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

// checkDeployPolicy enforces the instance's Docker options policy before
// anything else, on single-server and multi-server deployments alike: turning
// a switch off also stops the next deployment that depends on it.
func (d *Deployer) checkDeployPolicy(ctx context.Context, app Application) error {
	if err := d.store.checkDockerOptionsPolicy(ctx, app.Config); err != nil {
		return deployPolicyError(err)
	}
	return nil
}

// engineDeployPreflight runs before anything is built: the automatic domain.
func (d *Deployer) engineDeployPreflight(ctx context.Context, app *Application, target server.Server, ex executor.Executor, log *deployLog) error {
	assigned, err := d.store.ensureAutoDomain(ctx, app, target, ex)
	if err != nil {
		// A missing automatic domain never blocks a deployment.
		log.Info("Domínio automático não atribuído: %s", err)
		return nil
	}
	if assigned != "" {
		log.Info("Domínio automático atribuído: %s. Remova-o na configuração se não quiser usá-lo.", assigned)
	}
	return nil
}

// deployPolicyError keeps the field messages, which a deployment log would
// otherwise reduce to "review the highlighted fields".
func deployPolicyError(err error) error {
	appErr, ok := apperr.As(err)
	if !ok || len(appErr.Fields) == 0 {
		return err
	}
	messages := []string{}
	for _, message := range appErr.Fields {
		messages = append(messages, message)
	}
	slices.Sort(messages)
	return i18n.Errorf("a configuração foi bloqueada pela política da instância: %s", strings.Join(messages, " "))
}

// ensureAutoDomain gives an application without domains its automatic one,
// when the instance setting is on. It returns the URL it added, if any. A
// domain the user removed from the configuration is recorded as removed and
// never comes back on its own.
func (s *Store) ensureAutoDomain(ctx context.Context, app *Application, target server.Server, ex executor.Executor) (string, error) {
	settings, err := s.autoDomainSettings(ctx)
	if err != nil || !settings.Enabled || len(app.Config.Network.Domains) > 0 || !servesWeb(app.Config) {
		return "", err
	}
	record, err := s.autoDomain(ctx, app.ID)
	if err == nil {
		if !record.Removed {
			return "", s.markAutoDomainRemoved(ctx, app.ID)
		}
		return "", nil
	}
	if !errors.Is(err, sqlite.ErrNotFound) {
		return "", err
	}
	return s.assignAutoDomain(ctx, app, settings, target, ex)
}

// assignAutoDomain generates a free host, records it and adds it to the
// stored configuration.
func (s *Store) assignAutoDomain(ctx context.Context, app *Application, settings AutoDomainSettings, target server.Server, ex executor.Executor) (string, error) {
	var address netip.Addr
	if settings.BaseDomain == "" {
		var err error
		if address, err = serverPublicIPv4(ctx, target, ex); err != nil {
			return "", err
		}
	}
	environmentName, err := s.environmentName(ctx, app.EnvironmentID)
	if err != nil {
		return "", err
	}
	host, err := s.pickAutoDomain(ctx, *app, environmentName, settings, address)
	if err != nil {
		return "", err
	}
	if err := s.saveAutoDomain(ctx, app.ID, host); err != nil {
		return "", err
	}
	url := autoDomainURL(host, settings)
	app.Config.Network.Domains = append(app.Config.Network.Domains, url)
	if err := s.update(ctx, *app); err != nil {
		return "", err
	}
	return url, nil
}

// AutoDomain reports the automatic domain of an application.
func (s *Service) AutoDomain(ctx context.Context, applicationID string) (AutoDomain, error) {
	app, err := s.Get(ctx, applicationID)
	if err != nil {
		return AutoDomain{}, err
	}
	record, err := s.store.autoDomain(ctx, applicationID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return AutoDomain{ApplicationID: applicationID}, nil
	}
	if err != nil {
		return AutoDomain{}, err
	}
	record.Automatic = !record.Removed && configHasHost(app.Config, record.Domain)
	if url := automaticURL(app.Config, record.Domain); url != "" {
		record.URL = url
	}
	return record, nil
}

func automaticURL(config Config, host string) string {
	for _, domain := range config.Network.Domains {
		if configHasHost(Config{Network: Network{Domains: []string{domain}}}, host) {
			return domain
		}
	}
	return ""
}

// GenerateAutoDomain gives the application a new automatic domain now, even
// if an earlier one was removed. It needs the setting on and no domain yet.
func (s *Service) GenerateAutoDomain(ctx context.Context, applicationID string) (Application, error) {
	app, err := s.Get(ctx, applicationID)
	if err != nil {
		return Application{}, err
	}
	settings, err := s.store.autoDomainSettings(ctx)
	if err != nil {
		return Application{}, err
	}
	if !settings.Enabled {
		return Application{}, apperr.Conflict("O domínio automático está desligado nesta instância.")
	}
	if len(app.Config.Network.Domains) > 0 || !servesWeb(app.Config) {
		return Application{}, apperr.Conflict("A aplicação já tem domínio ou não tem um processo web.")
	}
	ex, target, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return Application{}, err
	}
	if _, err := s.store.assignAutoDomain(ctx, &app, settings, target, ex); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, applicationID)
}

// RemoveAutoDomain takes the automatic domain out of the configuration and
// remembers the decision. It takes effect on the next deployment.
func (s *Service) RemoveAutoDomain(ctx context.Context, applicationID string) (Application, error) {
	app, err := s.Get(ctx, applicationID)
	if err != nil {
		return Application{}, err
	}
	record, err := s.store.autoDomain(ctx, applicationID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return Application{}, apperr.NotFound("Domínio automático")
	}
	if err != nil {
		return Application{}, err
	}
	app.Config.Network.Domains = slices.DeleteFunc(slices.Clone(app.Config.Network.Domains), func(domain string) bool {
		return configHasHost(Config{Network: Network{Domains: []string{domain}}}, record.Domain)
	})
	if err := s.store.markAutoDomainRemoved(ctx, applicationID); err != nil {
		return Application{}, err
	}
	if err := s.store.update(ctx, app); err != nil {
		return Application{}, err
	}
	return s.Get(ctx, applicationID)
}

// automaticHosts are the automatic domains of an application, so a clone
// never copies a generated name that belongs to its source.
func (s *Store) automaticHosts(ctx context.Context, applicationID string) ([]string, error) {
	record, err := s.autoDomain(ctx, applicationID)
	if errors.Is(err, sqlite.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return []string{record.Domain}, nil
}
