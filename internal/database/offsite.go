package database

import (
	"context"
	"database/sql"
	"fmt"
	"path"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/shell"
	"pier/internal/settings"
)

// AWSCLIImage is the pinned image that talks to S3.
const AWSCLIImage = "amazon/aws-cli:2.22.35"

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

// Offsite copies backup files to the instance S3 destination. It is shared by
// every backup feature (databases, volumes) so the credentials handling, the
// pinned aws-cli image and the key layout live in one place.
type Offsite struct {
	db  *sql.DB
	box *secretbox.Box
}

// NewOffsite creates the S3 copier for the given settings store.
func NewOffsite(db *sql.DB, box *secretbox.Box) *Offsite { return &Offsite{db: db, box: box} }

// command uses a private env-file on the server holding the file. Credentials never enter argv.
func (o *Offsite) command(ctx context.Context, ex executor.Executor, directory string, args ...string) error {
	cfg, err := settings.NewS3Store(o.db, o.box).Get(ctx)
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
	command := []string{"docker", "run", "--rm", "--env-file", envPath, "-v", directory + ":/backups", AWSCLIImage, "--endpoint-url", cfg.Endpoint, "s3"}
	command = append(command, args...)
	_, err = ex.Run(ctx, shell.Join(command...))
	return err
}

// Upload copies file to <prefix>/<resourceKey>/<file name> and returns the
// s3:// URI of the object.
func (o *Offsite) Upload(ctx context.Context, ex executor.Executor, resourceKey, file string) (string, error) {
	cfg, err := settings.NewS3Store(o.db, o.box).Get(ctx)
	if err != nil {
		return "", err
	}
	key := strings.Trim(cfg.Prefix+"/"+resourceKey+"/"+path.Base(file), "/")
	if err := o.command(ctx, ex, path.Dir(file), "cp", "/backups/"+path.Base(file), "s3://"+cfg.Bucket+"/"+key); err != nil {
		return "", err
	}
	return "s3://" + cfg.Bucket + "/" + key, nil
}

// Remove deletes the object; an empty URI is a no-op. directory is any
// directory of the server (it receives the temporary credentials file).
func (o *Offsite) Remove(ctx context.Context, ex executor.Executor, directory, uri string) error {
	if uri == "" {
		return nil
	}
	return o.command(ctx, ex, directory, "rm", uri)
}

// Fetch downloads the object to file, creating the directory when needed.
func (o *Offsite) Fetch(ctx context.Context, ex executor.Executor, uri, file string) error {
	if _, err := ex.Run(ctx, "umask 077 && "+shell.Join("mkdir", "-p", path.Dir(file))); err != nil {
		return err
	}
	return o.command(ctx, ex, path.Dir(file), "cp", uri, "/backups/"+path.Base(file))
}

func (s *Service) offsite() *Offsite { return NewOffsite(s.store.db, s.store.box) }

func (s *Service) uploadBackup(ctx context.Context, database Database, id, file string) error {
	ex, _, err := s.connector.Executor(ctx, database.ServerID)
	if err != nil {
		return err
	}
	uri, err := s.offsite().Upload(ctx, ex, database.ID, file)
	if err != nil {
		return err
	}
	_, err = s.store.db.ExecContext(ctx, `UPDATE backup_executions SET remote_key=? WHERE id=?`, uri, id)
	return err
}
func (s *Service) removeRemote(ctx context.Context, b BackupExecution, ex executor.Executor) error {
	var key string
	if err := s.store.db.QueryRowContext(ctx, `SELECT remote_key FROM backup_executions WHERE id=?`, b.ID).Scan(&key); err != nil {
		return err
	}
	return s.offsite().Remove(ctx, ex, path.Dir(b.FilePath), key)
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
	return s.offsite().Fetch(ctx, ex, key, b.FilePath)
}

func (s *Service) s3Command(ctx context.Context, ex executor.Executor, directory string, args ...string) error {
	return s.offsite().command(ctx, ex, directory, args...)
}
