// Package registry stores encrypted credentials and scopes Docker authentication.
package registry

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/shell"
)

type Registry struct {
	ID       string `json:"id"`
	Host     string `json:"host"`
	Username string `json:"username"`
}
type Input struct {
	Registry
	Password string `json:"password"`
}
type Service struct {
	db  *sql.DB
	box *secretbox.Box
}

func New(db *sql.DB, box *secretbox.Box) *Service { return &Service{db, box} }

var hostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*(?::[0-9]{1,5})?$`)
var idPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

func (s *Service) Save(ctx context.Context, in Input) error {
	if !idPattern.MatchString(in.ID) || !hostPattern.MatchString(in.Host) || in.Username == "" || in.Password == "" || len(in.Password) > 16384 {
		return apperr.Invalid("Registro inválido.")
	}
	sealed, err := s.box.Seal(in.Password)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO registries VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET host=excluded.host,username=excluded.username,credential=excluded.credential`, in.ID, in.Host, in.Username, sealed)
	return err
}
func (s *Service) List(ctx context.Context) ([]Registry, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,host,username FROM registries ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Registry{}
	for rows.Next() {
		var r Registry
		if err = rows.Scan(&r.ID, &r.Host, &r.Username); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// WithDocker never places credentials in argv or returns executor output. The
// random directory is private; config is owner-only and removed even on cancel.
func (s *Service) WithDocker(ctx context.Context, id string, ex executor.Executor, fn func(string, string) error) (err error) {
	var host, user, sealed string
	if err = s.db.QueryRowContext(ctx, `SELECT host,username,credential FROM registries WHERE id=?`, id).Scan(&host, &user, &sealed); err != nil {
		return errors.New("Registro indisponível.")
	}
	password, e := s.box.Open(sealed)
	if e != nil {
		return errors.New("Credencial indisponível.")
	}
	dir, e := ex.Run(ctx, "umask 077; mktemp -d /tmp/pier-registry-XXXXXXXXXX")
	if e != nil || !regexp.MustCompile(`^/tmp/pier-registry-[a-zA-Z0-9]+$`).MatchString(dir) {
		return errors.New("Falha ao preparar autenticação.")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		if _, e := ex.Run(cleanup, "rm -rf -- "+shell.Quote(dir)); e != nil {
			err = errors.New("Falha ao remover autenticação temporária.")
		}
	}()
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
	content, _ := json.Marshal(map[string]any{"auths": map[string]any{host: map[string]string{"auth": auth}}})
	if e = ex.WriteFile(ctx, dir+"/config.json", content, 0600); e != nil {
		return errors.New("Falha ao preparar autenticação.")
	}
	// fn must use this prefix for every private push/pull, including rollback.
	if e = fn("docker --config "+shell.Quote(dir), host); e != nil {
		return errors.New("Operação no registro falhou.")
	}
	return nil
}

var digestPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.:-]*/[a-z0-9][a-z0-9/_.-]*@sha256:[a-f0-9]{64}$`)

func Immutable(image string) bool {
	return digestPattern.MatchString(image) && !strings.Contains(image, "..")
}

// Publish returns only a verified immutable reference belonging to repository.
func (s *Service) Publish(ctx context.Context, id string, ex executor.Executor, local, repository string) (string, error) {
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9/_.-]*$`).MatchString(repository) || strings.Contains(repository, "..") {
		return "", apperr.Invalid("Repositório inválido.")
	}
	var image string
	err := s.WithDocker(ctx, id, ex, func(docker, host string) error {
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return err
		}
		tag := host + "/" + repository + ":pier-" + hex.EncodeToString(nonce)
		if _, err := ex.Run(ctx, shell.Join("docker", "tag", local, tag)); err != nil {
			return err
		}
		if _, err := ex.Run(ctx, docker+" push "+shell.Quote(tag)); err != nil {
			return err
		}
		out, err := ex.Run(ctx, docker+" image inspect --format '{{json .RepoDigests}}' "+shell.Quote(tag))
		if err != nil {
			return err
		}
		var refs []string
		if err = json.Unmarshal([]byte(out), &refs); err != nil {
			return err
		}
		for _, ref := range refs {
			if strings.HasPrefix(ref, host+"/"+repository+"@") && Immutable(ref) {
				image = ref
				return nil
			}
		}
		return errors.New("digest missing")
	})
	return image, err
}
func (s *Service) Pull(ctx context.Context, id string, ex executor.Executor, image string) error {
	if !Immutable(image) {
		return apperr.Invalid("Use uma imagem por digest.")
	}
	return s.WithDocker(ctx, id, ex, func(docker, host string) error {
		if !strings.HasPrefix(image, host+"/") {
			return errors.New("host mismatch")
		}
		_, err := ex.Run(ctx, docker+" pull "+shell.Quote(image))
		return err
	})
}
