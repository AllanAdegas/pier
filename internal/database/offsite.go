package database

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/settings"
)

func (s *Service) S3Settings(ctx context.Context) (settings.S3Settings, error) {
	value, err := settings.NewS3Store(s.store.db, s.store.box).Get(ctx)
	value.AccessKey = ""
	value.SecretKey = ""
	return value, err
}
func (s *Service) UpdateS3Settings(ctx context.Context, value settings.S3Settings) error {
	store := settings.NewS3Store(s.store.db, s.store.box)
	old, err := store.Get(ctx)
	if err != nil {
		return err
	}
	if value.AccessKey == "" {
		value.AccessKey = old.AccessKey
	}
	if value.SecretKey == "" {
		value.SecretKey = old.SecretKey
	}
	return store.Update(ctx, value)
}

// s3Command uses a private env-file on the dump server. Credentials never enter argv.
func (s *Service) s3Command(ctx context.Context, ex executor.Executor, directory string, args ...string) error {
	cfg, err := settings.NewS3Store(s.store.db, s.store.box).Get(ctx)
	if err != nil {
		return err
	}
	if cfg.Bucket == "" {
		return apperr.Conflict("Configure o destino S3 antes de enviar backups.")
	}
	envPath := path.Join(directory, ".s3-"+idgen.New())
	data := fmt.Sprintf("AWS_ACCESS_KEY_ID=%s\nAWS_SECRET_ACCESS_KEY=%s\nAWS_DEFAULT_REGION=%s\nAWS_EC2_METADATA_DISABLED=true\n", cfg.AccessKey, cfg.SecretKey, cfg.Region)
	if err := ex.WriteFile(ctx, envPath, []byte(data), 0600); err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = ex.Run(c, shell.Join("rm", "-f", envPath))
	}()
	command := []string{"docker", "run", "--rm", "--env-file", envPath, "-v", directory + ":/backups", "amazon/aws-cli:2.22.35", "--endpoint-url", cfg.Endpoint, "s3"}
	command = append(command, args...)
	_, err = ex.Run(ctx, shell.Join(command...))
	return err
}
func (s *Service) uploadBackup(ctx context.Context, database Database, id, file string) error {
	cfg, err := settings.NewS3Store(s.store.db, s.store.box).Get(ctx)
	if err != nil {
		return err
	}
	key := strings.Trim(cfg.Prefix+"/"+database.ID+"/"+path.Base(file), "/")
	ex, _, err := s.connector.Executor(ctx, database.ServerID)
	if err != nil {
		return err
	}
	if err := s.s3Command(ctx, ex, path.Dir(file), "cp", "/backups/"+path.Base(file), "s3://"+cfg.Bucket+"/"+key); err != nil {
		return err
	}
	_, err = s.store.db.ExecContext(ctx, `UPDATE backup_executions SET remote_key=? WHERE id=?`, "s3://"+cfg.Bucket+"/"+key, id)
	return err
}
func (s *Service) removeRemote(ctx context.Context, b BackupExecution, ex executor.Executor) error {
	var key string
	if err := s.store.db.QueryRowContext(ctx, `SELECT remote_key FROM backup_executions WHERE id=?`, b.ID).Scan(&key); err != nil {
		return err
	}
	if key == "" {
		return nil
	}
	return s.s3Command(ctx, ex, path.Dir(b.FilePath), "rm", key)
}
func (s *Service) localBackup(ctx context.Context, b BackupExecution, ex executor.Executor) error {
	if _, err := ex.Run(ctx, shell.Join("test", "-s", b.FilePath)); err == nil {
		return nil
	}
	var key string
	if err := s.store.db.QueryRowContext(ctx, `SELECT remote_key FROM backup_executions WHERE id=?`, b.ID).Scan(&key); err != nil {
		return err
	}
	if key == "" {
		return apperr.Conflict("O arquivo de backup não está disponível no servidor nem no S3.")
	}
	if _, err := ex.Run(ctx, "umask 077 && "+shell.Join("mkdir", "-p", path.Dir(b.FilePath))); err != nil {
		return err
	}
	return s.s3Command(ctx, ex, path.Dir(b.FilePath), "cp", key, "/backups/"+path.Base(b.FilePath))
}
