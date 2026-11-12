package instance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"pier/internal/platform/apperr"
)

type BackupConfig struct {
	Directory     string `json:"directory"`
	Cron          string `json:"cron"`
	Retention     int    `json:"retention"`
	Passphrase    string `json:"passphrase,omitempty"`
	HasPassphrase bool   `json:"has_passphrase"`
}

func (s *Service) configuration(ctx context.Context) (BackupConfig, error) {
	var raw string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='instance_backup_config'`).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return BackupConfig{Cron: "0 3 * * *", Retention: 7}, nil
	}
	if err != nil {
		return BackupConfig{}, err
	}
	var c BackupConfig
	err = json.Unmarshal([]byte(raw), &c)
	return c, err
}
func (s *Service) Configuration(ctx context.Context) (BackupConfig, error) {
	c, err := s.configuration(ctx)
	c.HasPassphrase = c.Passphrase != ""
	c.Passphrase = ""
	return c, err
}
func (s *Service) Configure(ctx context.Context, c BackupConfig) (BackupConfig, error) {
	old, err := s.configuration(ctx)
	if err != nil {
		return BackupConfig{}, err
	}
	c.Directory = strings.TrimSpace(c.Directory)
	if c.Directory != "" && (!filepath.IsAbs(c.Directory) || filepath.Clean(c.Directory) == "/" || filepath.Clean(c.Directory) == filepath.Clean(s.DataDir)) {
		return BackupConfig{}, apperr.Invalid("Informe uma pasta absoluta exclusiva para os backups da instância.")
	}
	if _, err = cron.ParseStandard(c.Cron); err != nil || c.Retention < 1 || c.Retention > 365 {
		return BackupConfig{}, apperr.Invalid("Informe uma agenda cron válida e retenção entre 1 e 365 backups.")
	}
	if c.Passphrase != "" {
		if err = validPassphrase(c.Passphrase); err != nil {
			return BackupConfig{}, err
		}
		c.Passphrase, err = s.Box.Seal(c.Passphrase)
		if err != nil {
			return BackupConfig{}, err
		}
	} else {
		c.Passphrase = old.Passphrase
	}
	if c.Directory != "" && c.Passphrase == "" {
		return BackupConfig{}, apperr.Invalid("Defina a frase-senha antes de ativar o backup automático.")
	}
	c.HasPassphrase = false
	raw, err := json.Marshal(c)
	if err != nil {
		return BackupConfig{}, err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('instance_backup_config',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, string(raw))
	if err != nil {
		return BackupConfig{}, err
	}
	return s.Configuration(ctx)
}

// Run reevaluates saved configuration every minute; only successful scheduled
// writes advance the schedule marker. Missed runs retry on the next tick.
func (s *Service) Run(ctx context.Context, logger *slog.Logger) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			if err := s.runScheduled(ctx, now); err != nil {
				logger.Error("instance backup failed", "error", err)
			}
		}
	}
}
func (s *Service) runScheduled(ctx context.Context, now time.Time) error {
	c, err := s.configuration(ctx)
	if err != nil {
		return err
	}
	if c.Directory == "" {
		return nil
	}
	schedule, err := cron.ParseStandard(c.Cron)
	if err != nil {
		return err
	}
	var last string
	err = s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key='instance_last_scheduled_backup_at'`).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	baseline, err := time.Parse(time.RFC3339Nano, last)
	if err != nil {
		baseline = now.Add(-time.Minute)
	}
	if schedule.Next(baseline).After(now) {
		return nil
	}
	passphrase, err := s.Box.Open(c.Passphrase)
	if err != nil {
		return err
	}
	archive, err := s.Create(ctx, passphrase)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(c.Directory, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(c.Directory, ".pier-backup-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	_, err = file.Write(archive)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	destination := filepath.Join(c.Directory, "pier-instance-"+now.UTC().Format("20060102T150405.000000000Z")+".pierbak")
	if err = os.Rename(name, destination); err != nil {
		return err
	}
	if err = s.MarkComplete(ctx); err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('instance_last_scheduled_backup_at',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return prune(c.Directory, c.Retention)
}
func prune(dir string, keep int) error {
	files, err := filepath.Glob(filepath.Join(dir, "pier-instance-*.pierbak"))
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, name := range files[:max(0, len(files)-keep)] {
		if err = os.Remove(name); err != nil {
			return err
		}
	}
	return nil
}
