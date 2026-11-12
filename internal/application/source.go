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

	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

const (
	// gitImage runs git inside a container so servers only need Docker.
	gitImage     = "alpine/git:v2.54.0"
	commitMarker = "PIER_COMMIT "
	sourceDir    = "src"
	deployKey    = "deploy_key"
)

const cloneScript = `set -e
git config --global --add safe.directory '*'
git config --global advice.detachedHead false
if [ -d /trust ]; then
  if [ -s /trust/known_hosts ]; then trust=yes; else trust=accept-new; fi
  export GIT_SSH_COMMAND="ssh -i /tmp/deploy_key -o IdentitiesOnly=yes -o StrictHostKeyChecking=$trust -o UserKnownHostsFile=/trust/known_hosts"
fi
git clone --depth 1 --branch "$PIER_BRANCH" --recurse-submodules --shallow-submodules -- "$PIER_REPOSITORY" /work/src
cd /work/src
if [ -n "$PIER_COMMIT" ]; then
  git fetch --depth 1 origin "$PIER_COMMIT"
  git checkout --quiet FETCH_HEAD
  # Submodules were cloned at the branch tip: move them to the commits the
  # pinned commit records, or the build would mix two versions.
  if [ -f .gitmodules ]; then
    git submodule sync --recursive
    git submodule update --init --recursive --depth 1 || git submodule update --init --recursive
  fi
fi
echo "PIER_COMMIT $(git log -1 --format='%H %s')"`

// fetchSource clones the repository into the build directory and returns the
// commit that was checked out.
func (j *job) fetchSource(ctx context.Context, privateKey string) (sha, message string, err error) {
	release, acquireErr := acquireBuild(ctx, j.app.ServerID, j.log)
	if acquireErr != nil {
		return "", "", acquireErr
	}
	defer release()
	source := j.app.Config.Source
	buildDir := j.server.BuildDir(j.deployment.ID)
	if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", buildDir)); err != nil {
		return "", "", fmt.Errorf("create build directory: %w", err)
	}
	envPath := path.Join(buildDir, "clone.env")
	secretPaths := []string{envPath}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		if cleanupErr := removeBuildCredentials(cleanupCtx, j.ex, secretPaths...); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
	}()
	ref, commit := j.checkoutRef()
	values := map[string]string{
		"PIER_REPOSITORY": source.Repository, "PIER_BRANCH": ref,
		"PIER_COMMIT": commit, "GIT_TERMINAL_PROMPT": "0",
	}
	if parsed, parseErr := url.Parse(source.Repository); parseErr == nil && parsed.User != nil {
		password, _ := parsed.User.Password()
		authorization := "Basic " + base64.StdEncoding.EncodeToString([]byte(parsed.User.Username()+":"+password))
		j.log.redact(password)
		j.log.redact(authorization)
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
	if err := j.ex.WriteFile(ctx, envPath, content, 0o600); err != nil {
		return "", "", err
	}
	args := []string{"docker", "run", "--rm", "--volume", buildDir + ":/work", "--env-file", envPath}
	if privateKey != "" {
		keyPath := path.Join(buildDir, deployKey)
		secretPaths = append(secretPaths, keyPath)
		if err := j.ex.WriteFile(ctx, keyPath, []byte(privateKey), 0o600); err != nil {
			return "", "", err
		}
		trustDir := path.Join(j.server.ResourceDir("application", j.app.ID), "git")
		if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", trustDir)); err != nil {
			return "", "", err
		}
		args = append(args, "--volume", keyPath+":/tmp/deploy_key:ro", "--volume", trustDir+":/trust")
	}
	args = append(args, "--entrypoint", "sh", gitImage, "-c", cloneScript)

	j.log.Command(fmt.Sprintf("git clone --depth 1 --branch %s %s", ref, withoutCredentials(source.Repository)))
	hostKeyFailed := false
	sink := func(line string) {
		if strings.Contains(line, "REMOTE HOST IDENTIFICATION HAS CHANGED") || strings.Contains(line, "Host key verification failed") {
			hostKeyFailed = true
		}
		if commit, found := strings.CutPrefix(line, commitMarker); found {
			sha, message, _ = strings.Cut(commit, " ")
			return
		}
		j.log.Output(line)
	}
	if err := j.ex.Stream(ctx, shell.Join(args...), sink); err != nil {
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

// checkoutRef is what the clone checks out: the tag requested for this
// deployment, the fixed tag of the source, or the branch with its optional
// pinned commit. "git clone --branch" accepts tags as well as branches.
func (j *job) checkoutRef() (ref, commit string) {
	if j.deployment.Ref != "" {
		return j.deployment.Ref, ""
	}
	if tag := j.app.Config.Source.Tag; tag != "" {
		return tag, ""
	}
	return j.app.Config.Source.Branch, j.app.Config.Source.Commit
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

// ensureInsideSource checks the build paths and every nested symlink before
// handing the checkout to a builder that could follow links to host files.
func (j *job) ensureInsideSource(ctx context.Context) error {
	checkout := path.Join(j.server.BuildDir(j.deployment.ID), sourceDir)
	targets := []string{j.contextDir()}
	if j.app.Config.Build.Pack == PackDockerfile {
		targets = append(targets, path.Join(j.contextDir(), j.app.Config.Build.DockerfilePath))
	}
	command := shell.Join(append([]string{"sh", "-c", containmentScript, "sh", checkout}, targets...)...)
	output, err := j.ex.Run(ctx, command)
	if err != nil {
		return i18n.Errorf("o diretório base ou o Dockerfile não existe no repositório: %w", err)
	}
	if link, found := strings.CutPrefix(strings.TrimSpace(output), "outside:"); found {
		return i18n.Errorf("o link simbólico %s aponta para fora do repositório ou não pode ser resolvido", strings.SplitN(link, "\n", 2)[0])
	}
	if strings.TrimSpace(output) == "outside" {
		return i18n.Errorf("o diretório base ou o Dockerfile aponta, por link simbólico, para fora do repositório")
	}
	return nil
}

// contextDir is the directory the image is built from.
func (j *job) contextDir() string {
	return path.Join(j.server.BuildDir(j.deployment.ID), sourceDir, j.app.Config.Build.BaseDirectory)
}

// cleanupBuildDir removes the scratch directory through a container, because
// files created by the clone may belong to root.
func (j *job) cleanupBuildDir(ctx context.Context) {
	buildsRoot := path.Dir(j.server.BuildDir(j.deployment.ID))
	command := shell.Join("docker", "run", "--rm", "--volume", buildsRoot+":/builds",
		"--entrypoint", "rm", gitImage, "-rf", "/builds/"+j.deployment.ID)
	_, _ = j.ex.Run(ctx, command)
}
