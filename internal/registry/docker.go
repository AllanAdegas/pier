package registry

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

const configCleanupTimeout = 10 * time.Second

var configDirPattern = regexp.MustCompile(`^/tmp/pier-registry-[A-Za-z0-9]+$`)

// Output receives the lines Docker prints, already free of the password.
type Output = executor.LineSink

// Publish tags a local image, pushes it to repository on the registry and
// returns the immutable reference (host/repository@sha256:…) the registry
// confirmed. Only that reference is ever distributed to other servers.
func (s *Service) Publish(ctx context.Context, registryID string, ex executor.Executor, local, repository, tag string, output Output) (string, error) {
	if !ValidRepository(repository) {
		return "", apperr.Invalid("Repositório de imagens inválido.")
	}
	if !tagPattern.MatchString(tag) {
		return "", apperr.Invalid("Tag de imagem inválida.")
	}
	found, err := s.credentialByID(ctx, registryID)
	if err != nil {
		return "", apperr.Unavailable("O registro de imagens configurado não existe mais.")
	}
	name := found.host + "/" + repository
	if _, err := ex.Run(ctx, shell.Join("docker", "tag", local, name+":"+tag)); err != nil {
		return "", i18n.Errorf("marcar a imagem para o registro: %w", err)
	}
	err = s.withConfig(ctx, ex, found, func(docker string) error {
		return ex.Stream(ctx, docker+" push "+shell.Quote(name+":"+tag), masked(output, found.password))
	})
	if err != nil {
		return "", i18n.Errorf("enviar a imagem para %s: %w", found.host, err)
	}
	digest, err := s.Digest(ctx, ex, name+":"+tag)
	if err != nil {
		return "", err
	}
	return digest, nil
}

// Pull downloads an image with the credential stored for its host or, when
// there is none, exactly as a plain `docker pull`.
func (s *Service) Pull(ctx context.Context, ex executor.Executor, image string, output Output) error {
	found, ok, err := s.credentialForHost(ctx, HostOf(image))
	if err != nil {
		return err
	}
	if !ok {
		return ex.Stream(ctx, shell.Join("docker", "pull", image), masked(output, ""))
	}
	return s.withConfig(ctx, ex, found, func(docker string) error {
		return ex.Stream(ctx, docker+" pull "+shell.Quote(image), masked(output, found.password))
	})
}

// Digest returns the canonical digest reference the registry recorded for a
// local image of the same repository; an image never pushed or pulled from
// a registry has none.
func (s *Service) Digest(ctx context.Context, ex executor.Executor, image string) (string, error) {
	raw, err := ex.Run(ctx, shell.Join("docker", "image", "inspect", "--format", "{{json .RepoDigests}}", image))
	if err != nil {
		return "", i18n.Errorf("ler o digest da imagem %s: %w", image, err)
	}
	var references []string
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &references); err != nil {
		return "", i18n.Errorf("resposta inesperada do docker image inspect para %s", image)
	}
	want := repositoryOf(Canonical(image))
	for _, reference := range references {
		reference = Canonical(reference)
		if repositoryOf(reference) == want && Immutable(reference) {
			return reference, nil
		}
	}
	return "", errNoDigest
}

// errNoDigest means the image exists only locally.
var errNoDigest = i18n.Errorf("a imagem não tem digest de registro")

// IsNoDigest reports whether Digest failed because the image is local only.
func IsNoDigest(err error) bool { return errors.Is(err, errNoDigest) }

// withConfig runs fn with a private Docker client configuration holding only
// this credential: a fresh directory (0700) with config.json (0600), removed
// afterwards even when ctx is cancelled. The global Docker login of the
// server is never touched and the password never reaches a command line.
func (s *Service) withConfig(ctx context.Context, ex executor.Executor, found credential, fn func(docker string) error) (err error) {
	dir, err := ex.Run(ctx, "umask 077; mktemp -d /tmp/pier-registry-XXXXXXXXXX")
	dir = strings.TrimSpace(dir)
	if err != nil || !configDirPattern.MatchString(dir) {
		return i18n.Errorf("não foi possível preparar a autenticação no registro")
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), configCleanupTimeout)
		defer cancel()
		if _, removeErr := ex.Run(cleanup, shell.Join("rm", "-rf", "--", dir)); removeErr != nil && err == nil {
			err = i18n.Errorf("não foi possível remover a autenticação temporária do registro")
		}
	}()
	if err := ex.WriteFile(ctx, dir+"/config.json", dockerConfig(found), 0o600); err != nil {
		return i18n.Errorf("não foi possível preparar a autenticação no registro")
	}
	return fn("docker --config " + shell.Quote(dir))
}

func dockerConfig(found credential) []byte {
	token := base64.StdEncoding.EncodeToString([]byte(found.username + ":" + found.password))
	auths := map[string]map[string]string{}
	for _, key := range authKeys(found.host) {
		auths[key] = map[string]string{"auth": token}
	}
	content, _ := json.Marshal(map[string]any{"auths": auths})
	return content
}

// masked hides the password should a registry ever echo it.
func masked(output Output, password string) executor.LineSink {
	return func(line string) {
		if output == nil {
			return
		}
		if password != "" {
			line = strings.ReplaceAll(line, password, "••••••")
		}
		output(line)
	}
}
