package application

import (
	"context"
	"fmt"
	"path"
	"strings"

	"pier/internal/platform/shell"
)

const (
	// gitImage runs git inside a container so servers only need Docker.
	gitImage     = "alpine/git:latest"
	commitMarker = "PIER_COMMIT "
	sourceDir    = "src"
	deployKey    = "deploy_key"
)

const cloneScript = `set -e
git config --global --add safe.directory '*'
git config --global advice.detachedHead false
git clone --depth 1 --branch "$PIER_BRANCH" --recurse-submodules --shallow-submodules -- "$PIER_REPOSITORY" /work/src
cd /work/src
if [ -n "$PIER_COMMIT" ]; then
  git fetch --depth 1 origin "$PIER_COMMIT"
  git checkout --quiet FETCH_HEAD
fi
echo "PIER_COMMIT $(git log -1 --format='%H %s')"`

// fetchSource clones the repository into the build directory and returns the
// commit that was checked out.
func (j *job) fetchSource(ctx context.Context, privateKey string) (sha, message string, err error) {
	source := j.app.Config.Source
	buildDir := j.server.BuildDir(j.deployment.ID)
	if _, err := j.ex.Run(ctx, shell.Join("mkdir", "-p", buildDir)); err != nil {
		return "", "", fmt.Errorf("create build directory: %w", err)
	}
	args := []string{"docker", "run", "--rm", "--volume", buildDir + ":/work",
		"--env", "PIER_REPOSITORY=" + source.Repository,
		"--env", "PIER_BRANCH=" + source.Branch,
		"--env", "PIER_COMMIT=" + source.Commit,
		"--env", "GIT_TERMINAL_PROMPT=0",
	}
	if privateKey != "" {
		keyPath := path.Join(buildDir, deployKey)
		if err := j.ex.WriteFile(ctx, keyPath, []byte(privateKey), 0o600); err != nil {
			return "", "", err
		}
		args = append(args, "--volume", keyPath+":/tmp/deploy_key:ro", "--env",
			"GIT_SSH_COMMAND=ssh -i /tmp/deploy_key -o IdentitiesOnly=yes "+
				"-o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/dev/null")
	}
	args = append(args, "--entrypoint", "sh", gitImage, "-c", cloneScript)

	j.log.Command(fmt.Sprintf("git clone --depth 1 --branch %s %s", source.Branch, source.Repository))
	sink := func(line string) {
		if commit, found := strings.CutPrefix(line, commitMarker); found {
			sha, message, _ = strings.Cut(commit, " ")
			return
		}
		j.log.Output(line)
	}
	if err := j.ex.Stream(ctx, shell.Join(args...), sink); err != nil {
		return "", "", fmt.Errorf("git clone falhou: %w", err)
	}
	if sha == "" {
		return "", "", fmt.Errorf("git clone não informou o commit obtido")
	}
	return sha, message, nil
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
