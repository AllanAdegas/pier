package application

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// CloneLog receives what a clone says. A deployment log satisfies it, and so
// does the progress of another resource that clones a repository (stacks).
type CloneLog interface {
	Info(format string, args ...any)
	Command(display string)
	Output(line string)
	// Redact registers values that must never appear in the output.
	Redact(values ...string)
}

// CloneRequest describes one shallow clone made on a server.
type CloneRequest struct {
	// Repository may carry HTTPS credentials (https://user:token@host/...).
	Repository string
	// Ref is a branch or a tag; Commit optionally pins a commit of that ref.
	Ref, Commit string
	// PrivateKey is the deploy key for SSH repositories; empty otherwise.
	PrivateKey string
	// ServerID selects the build queue the clone waits in.
	ServerID string
	// BuildDir is a scratch directory; the checkout lands in CheckoutPath(BuildDir).
	BuildDir string
	// TrustDir keeps the pinned Git host keys of the owner of the clone.
	// Only used with PrivateKey.
	TrustDir string
}

// CheckoutPath is where Clone puts the working tree inside the scratch directory.
func CheckoutPath(buildDir string) string { return path.Join(buildDir, sourceDir) }

// Clone shallow-clones a repository on the server of ex, inside a container,
// and returns the commit that was checked out. Credentials are written to
// files that are removed before it returns, and never reach the checkout.
func Clone(ctx context.Context, ex executor.Executor, request CloneRequest, log CloneLog) (sha, message string, err error) {
	release, acquireErr := acquireBuild(ctx, request.ServerID, log)
	if acquireErr != nil {
		return "", "", acquireErr
	}
	defer release()
	if _, err := ex.Run(ctx, shell.Join("mkdir", "-p", request.BuildDir)); err != nil {
		return "", "", fmt.Errorf("create build directory: %w", err)
	}
	envPath := path.Join(request.BuildDir, "clone.env")
	secretPaths := []string{envPath}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if cleanupErr := removeBuildCredentials(cleanupCtx, ex, secretPaths...); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	values := map[string]string{
		"PIER_REPOSITORY": request.Repository, "PIER_BRANCH": request.Ref,
		"PIER_COMMIT": request.Commit, "GIT_TERMINAL_PROMPT": "0",
	}
	if parsed, parseErr := url.Parse(request.Repository); parseErr == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(parsed.User.Username()+":"+password))
		log.Redact(password)
		log.Redact(authorization)
		parsed.User = nil
		values["PIER_REPOSITORY"] = parsed.String()
		// Git's process-only config also applies to relative submodule URLs.
		values["GIT_CONFIG_COUNT"] = "1"
		values["GIT_CONFIG_KEY_0"] = "http." + parsed.Scheme + "://" + parsed.Host + "/.extraHeader"
		values["GIT_CONFIG_VALUE_0"] = "Authorization: " + authorization
	}
	content, envErr := dockerEnvFile(values)
	if envErr != nil {
		return "", "", envErr
	}
	if err := ex.WriteFile(ctx, envPath, content, 0o600); err != nil {
		return "", "", err
	}
	args := []string{"docker", "run", "--rm", "--volume", request.BuildDir + ":/work", "--env-file", envPath}
	if request.PrivateKey != "" {
		keyPath := path.Join(request.BuildDir, deployKey)
		secretPaths = append(secretPaths, keyPath)
		if err := ex.WriteFile(ctx, keyPath, []byte(request.PrivateKey), 0o600); err != nil {
			return "", "", err
		}
		if _, err := ex.Run(ctx, shell.Join("mkdir", "-p", request.TrustDir)); err != nil {
			return "", "", err
		}
		args = append(args, "--volume", keyPath+":/tmp/deploy_key:ro", "--volume", request.TrustDir+":/trust")
	}
	args = append(args, "--entrypoint", "sh", gitImage, "-c", cloneScript)

	log.Command(fmt.Sprintf("git clone --depth 1 --branch %s %s", request.Ref, withoutCredentials(request.Repository)))
	hostKeyFailed := false
	sink := func(line string) {
		if strings.Contains(line, "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(line, "Host key verification failed") {
			hostKeyFailed = true
		}
		if commit, found := strings.CutPrefix(line, commitMarker); found {
			sha, message, _ = strings.Cut(commit, " ")
			return
		}
		log.Output(line)
	}
	if err := ex.Stream(ctx, shell.Join(args...), sink); err != nil {
		if hostKeyFailed {
			return "", "", i18n.Errorf("a chave SSH do host Git mudou ou não é confiável. Confirme a chave com o administrador do repositório e redefina a confiança Git da aplicação antes de tentar novamente: %w", err)
		}
		return "", "", i18n.Errorf("git clone falhou: %w", err)
	}
	if sha == "" {
		return "", "", i18n.Errorf("git clone não informou o commit obtido")
	}
	return sha, message, nil
}

// containmentScript prints "outside" when a path, once symbolic links are
// resolved, leaves the checkout. Arguments: the checkout, then the paths.
const containmentScript = `src=$(readlink -f -- "$1") || exit 1
shift
for target in "$@"; do
  resolved=$(readlink -f -- "$target") || exit 1
  case "$resolved/" in "$src/"*) ;; *) echo outside; exit 0 ;; esac
done
# Check every nested link, including dangling links and symlinked directories.
find "$src" -type l -exec sh -c '
  src=$1; link=$2
  resolved=$(readlink -f -- "$link") || { printf "outside:%s\n" "$link"; exit; }
  case "$resolved/" in "$src/"*) ;; *) printf "outside:%s\n" "$link" ;; esac
' sh "$src" {} \;`

// Containment is the verdict of VerifyContained.
type Containment struct {
	// Missing is why a path could not be resolved (usually: it does not exist).
	Missing error
	// Escaping is the first symbolic link that resolves outside the checkout.
	Escaping string
	// PathEscapes is set when one of the paths itself resolves outside.
	PathEscapes bool
}

// Contained reports whether nothing escapes the checkout.
func (c Containment) Contained() bool { return c.Missing == nil && c.Escaping == "" && !c.PathEscapes }

// VerifyContained checks, on the server, that the given paths and every
// symbolic link of the checkout resolve inside it.
func VerifyContained(ctx context.Context, ex executor.Executor, checkout string, targets ...string) (Containment, error) {
	command := shell.Join(append([]string{"sh", "-c", containmentScript, "sh", checkout}, targets...)...)
	output, err := ex.Run(ctx, command)
	if err != nil {
		return Containment{Missing: err}, nil
	}
	if link, found := strings.CutPrefix(strings.TrimSpace(output), "outside:"); found {
		return Containment{Escaping: strings.SplitN(link, "\n", 2)[0]}, nil
	}
	return Containment{PathEscapes: strings.TrimSpace(output) == "outside"}, nil
}

// RemoveDirectory deletes name inside parent through a container, because
// files created by a clone may belong to root.
func RemoveDirectory(ctx context.Context, ex executor.Executor, parent, name string) error {
	command := shell.Join("docker", "run", "--rm", "--volume", parent+":/parent",
		"--entrypoint", "rm", gitImage, "-rf", "/parent/"+name)
	_, err := ex.Run(ctx, command)
	return err
}
