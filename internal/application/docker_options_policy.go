package application

import (
	"context"
	"database/sql"
	"strings"

	"pier/internal/platform/apperr"
)

// DockerOptionsPolicy is the instance-wide decision on which container
// options applications may use. Both switches are off by default; only an
// instance owner may turn them on (enforced by the HTTP layer).
type DockerOptionsPolicy struct {
	// AllowDangerous permits privileged mode, host network/pid, kernel-level
	// capabilities, raw disks and Docker socket mounts.
	AllowDangerous bool `json:"allow_dangerous"`
	// AllowDevices permits mapping host devices (e.g. a GPU or /dev/ttyUSB0).
	AllowDevices bool `json:"allow_devices"`
}

const (
	settingDockerAllowDangerous = "engine_docker_allow_dangerous"
	settingDockerAllowDevices   = "engine_docker_allow_devices"
)

// DockerOptionsPolicy returns the current instance policy.
func (s *Service) DockerOptionsPolicy(ctx context.Context) (DockerOptionsPolicy, error) {
	return s.store.dockerOptionsPolicy(ctx)
}

// SetDockerOptionsPolicy saves the instance policy. Callers must have checked
// that the caller is an instance owner.
func (s *Service) SetDockerOptionsPolicy(ctx context.Context, policy DockerOptionsPolicy) (DockerOptionsPolicy, error) {
	err := s.store.putSettings(ctx, map[string]string{
		settingDockerAllowDangerous: formatBool(policy.AllowDangerous),
		settingDockerAllowDevices:   formatBool(policy.AllowDevices),
	})
	return policy, err
}

func (s *Store) dockerOptionsPolicy(ctx context.Context) (DockerOptionsPolicy, error) {
	values, err := s.settings(ctx, settingDockerAllowDangerous, settingDockerAllowDevices)
	if err != nil {
		return DockerOptionsPolicy{}, err
	}
	return DockerOptionsPolicy{
		AllowDangerous: values[settingDockerAllowDangerous] == "true",
		AllowDevices:   values[settingDockerAllowDevices] == "true",
	}, nil
}

// checkDockerOptionsPolicy refuses a configuration the instance does not allow.
// It runs when a configuration is saved and again before every deployment, so
// turning a switch off also stops the next deployment that depends on it.
func (s *Store) checkDockerOptionsPolicy(ctx context.Context, config Config) error {
	policy, err := s.dockerOptionsPolicy(ctx)
	if err != nil {
		return err
	}
	return dockerOptionsAllowed(config, policy)
}

func dockerOptionsAllowed(config Config, policy DockerOptionsPolicy) error {
	fields := apperr.Fields{}
	if len(config.DockerOptions.Devices) > 0 && !policy.AllowDevices {
		fields.Add("docker_options.devices", "Esta instância não permite mapear dispositivos do servidor. Um proprietário pode liberar em Configurações.")
	}
	if dangerous := config.DangerousDockerOptions(); len(dangerous) > 0 && !policy.AllowDangerous {
		fields.Add("docker_options", "Opções perigosas bloqueadas nesta instância: %s. Elas dão ao contêiner controle sobre o servidor; só um proprietário pode liberá-las em Configurações.", strings.Join(dangerous, ", "))
	}
	return fields.Err()
}

// settings reads instance settings stored by the engine in the shared
// key/value table; missing keys are absent from the map.
func (s *Store) settings(ctx context.Context, keys ...string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		var value string
		err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&value)
		if err == sql.ErrNoRows {
			continue
		}
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func (s *Store) putSettings(ctx context.Context, values map[string]string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range values {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings (key, value) VALUES (?, ?)
			ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func formatBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}
