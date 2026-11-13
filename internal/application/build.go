package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

const staticBaseImage = "nginx:1.31.8-alpine"

// builder turns the fetched source into an image and returns its reference.
type builder interface {
	build(ctx context.Context, j *job) (image string, err error)
}

func builderFor(pack BuildPack) (builder, error) {
	switch pack {
	case PackDockerfile:
		return guardedBuilder{dockerfileBuilder{}}, nil
	case PackStatic:
		return guardedBuilder{staticBuilder{}}, nil
	case PackNixpacks:
		return guardedBuilder{nixpacksBuilder{}}, nil
	case PackRailpack:
		return guardedBuilder{railpackBuilder{}}, nil
	case PackImage:
		return imageBuilder{}, nil
	}
	return nil, i18n.Errorf("build pack desconhecido: %q", pack)
}

// imageTag names the image built for a deployment.
func (j *job) imageTag() string { return "pier/" + j.app.ID + ":" + j.deployment.ID }

// buildTimeVariables are the variables marked to exist during the build,
// sorted by name. The others are runtime-only: build steps, which run code
// from the repository and its dependencies, never see them.
func (j *job) buildTimeVariables() []envvar.Variable {
	variables := []envvar.Variable{}
	for _, variable := range j.env {
		if variable.BuildTime || variable.SecretBuild {
			variables = append(variables, variable)
		}
	}
	sort.Slice(variables, func(a, b int) bool { return variables[a].Key < variables[b].Key })
	return variables
}

func (j *job) buildArgs() []string {
	args := []string{}
	for _, variable := range j.buildTimeVariables() {
		if variable.SecretBuild {
			args = append(args, "--secret", "id="+variable.Key+",env="+variable.Key)
		} else {
			args = append(args, "--build-arg", variable.Key)
		}
	}
	return args
}

func (j *job) dockerBuild(ctx context.Context, dockerfile string) (string, error) {
	image := j.imageTag()
	builderName, err := j.createBuildKit(ctx)
	if err != nil {
		return "", err
	}
	args := []string{"docker", "buildx", "build", "--builder", builderName, "--load", "--progress", "plain", "--tag", image, "--file", dockerfile,
		"--label", docker.LabelManaged + "=true", "--label", docker.LabelID + "=" + j.app.ID}

	if j.app.Config.Build.Pack == PackDockerfile && j.app.Config.Build.Target != "" {
		args = append(args, "--target", j.app.Config.Build.Target)
	}
	buildArgs := j.buildArgs()
	if j.app.Config.Build.Pack == PackRailpack {
		buildArgs = j.railpackBuildArgs()
	}
	args = append(args, buildArgs...)
	args = append(args, j.contextDir())

	display := fmt.Sprintf("docker build --tag %s --file %s", image, path.Base(dockerfile))
	if len(buildArgs) > 0 {
		display += fmt.Sprintf(" (+%d build args)", len(buildArgs)/2)
	}
	j.log.Command(display)
	if err := j.streamBuildEnvironment(ctx, args, j.log.Output); err != nil {
		return "", i18n.Errorf("docker build falhou: %w", err)
	}
	pruneCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
	defer cancel()
	if _, err := j.ex.Run(pruneCtx, shell.Join("docker", "buildx", "prune", "--builder", builderName, "--keep-storage", fmt.Sprintf("%dGB", buildLimits.Load().CacheGB), "-f")); err != nil {
		j.log.Info("Não foi possível limpar o cache de build: %s", err)
	}
	return image, nil
}

// dockerfileBuilder builds the Dockerfile found in the repository.
type dockerfileBuilder struct{}

func (dockerfileBuilder) build(ctx context.Context, j *job) (string, error) {
	return j.dockerBuild(ctx, path.Join(j.contextDir(), j.app.Config.Build.DockerfilePath))
}

// staticBuilder serves a directory of the repository with nginx.
type staticBuilder struct{}

func (staticBuilder) build(ctx context.Context, j *job) (string, error) {
	dockerfile := path.Join(j.server.BuildDir(j.deployment.ID), "Dockerfile.static")
	content, err := staticDockerfile(j.app.Config.Build)
	if err != nil {
		return "", err
	}
	if err := j.ex.WriteFile(ctx, dockerfile, []byte(content), 0o644); err != nil {
		return "", err
	}
	j.log.Info("Site estático: servindo %q com %s.", j.app.Config.Build.PublishDirectory, staticBaseImage)
	return j.dockerBuild(ctx, dockerfile)
}

func staticDockerfile(build Build) (string, error) {
	source := "." + strings.TrimSuffix(build.PublishDirectory, "/") + "/"
	copyArgs, err := json.Marshal([]string{source, "/usr/share/nginx/html/"})
	if err != nil {
		return "", err
	}
	fallback := "=404"
	if build.SPA {
		fallback = "/index.html"
	}
	return fmt.Sprintf(`# syntax=docker/dockerfile:1.19
FROM %s
COPY <<'EOF' /etc/nginx/conf.d/default.conf
server {
    listen 80;
    root /usr/share/nginx/html;
    index index.html;
    gzip on;
    gzip_types text/plain text/css application/json application/javascript image/svg+xml;
    location / {
        try_files $uri $uri/ %s;
    }
}
EOF
COPY --exclude=.git --exclude=**/.git %s
`, staticBaseImage, fallback, copyArgs), nil
}

// nixpacksBuilder detects the stack and builds without a Dockerfile. It needs
// the nixpacks CLI installed on the server.
type nixpacksBuilder struct{}

func (nixpacksBuilder) build(ctx context.Context, j *job) (string, error) {
	if _, err := j.ex.Run(ctx, "command -v nixpacks"); err != nil {
		return "", i18n.Errorf("o Nixpacks não está instalado no servidor. Instale com " +
			"`curl -sSL https://nixpacks.com/install.sh | bash` ou use o build pack Dockerfile")
	}
	return j.nixpacksBuild(ctx)

}

// imageBuilder pulls a ready-made image instead of building.
type imageBuilder struct{}

func (imageBuilder) build(ctx context.Context, j *job) (string, error) {
	image := j.app.Config.Source.Image
	j.log.Command("docker pull " + image)
	if err := j.pullImage(ctx, image); err != nil {
		// A registry outage or rate limit must not block a deploy whose image
		// is already on the server.
		if ctx.Err() != nil || !docker.ImageExists(ctx, j.ex, image) {
			return "", i18n.Errorf("docker pull falhou: %w", err)
		}
		j.log.Info("Não foi possível atualizar a imagem no registry; usando a cópia local.")
	}
	return image, nil
}
