package application

import (
	"context"
	"path"

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
	ref, commit := j.checkoutRef()
	return CloneGitProvider(ctx, j.gitProvider, "application", j.deployment.ApplicationID, j.ex, CloneRequest{
		Repository: j.app.Config.Source.Repository, Ref: ref, Commit: commit, PrivateKey: privateKey,
		ServerID: j.app.ServerID, BuildDir: j.server.BuildDir(j.deployment.ID),
		TrustDir: path.Join(j.server.ResourceDir("application", j.app.ID), "git"),
	}, j.log)
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

// ensureInsideSource checks the build paths and every nested symlink before
// handing the checkout to a builder that could follow links to host files.
func (j *job) ensureInsideSource(ctx context.Context) error {
	targets := []string{j.contextDir()}
	if j.app.Config.Build.Pack == PackDockerfile {
		targets = append(targets, path.Join(j.contextDir(), j.app.Config.Build.DockerfilePath))
	}
	verdict, err := VerifyContained(ctx, j.ex, CheckoutPath(j.server.BuildDir(j.deployment.ID)), targets...)
	if err != nil {
		return err
	}
	switch {
	case verdict.Missing != nil:
		return i18n.Errorf("o diretório base ou o Dockerfile não existe no repositório: %w", verdict.Missing)
	case verdict.Escaping != "":
		return i18n.Errorf("o link simbólico %s aponta para fora do repositório ou não pode ser resolvido", verdict.Escaping)
	case verdict.PathEscapes:
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
