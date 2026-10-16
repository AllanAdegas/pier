package application

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/shell"
)

const staticBaseImage = "nginx:alpine"

// builder turns the fetched source into an image and returns its reference.
type builder interface {
	build(ctx context.Context, j *job) (image string, err error)
}

func builderFor(pack BuildPack) (builder, error) {
	switch pack {
	case PackDockerfile:
		return dockerfileBuilder{}, nil
	case PackStatic:
		return staticBuilder{}, nil
	case PackNixpacks:
		return nixpacksBuilder{}, nil
	case PackImage:
		return imageBuilder{}, nil
	}
	return nil, fmt.Errorf("build pack desconhecido: %q", pack)
}

// imageTag names the image built for a deployment.
func (j *job) imageTag() string { return "pier/" + j.app.ID + ":" + j.deployment.ID }

func (j *job) buildArgs() []string {
	keys := make([]string, 0, len(j.env))
	values := map[string]string{}
	for _, variable := range j.env {
		if variable.BuildTime {
			keys = append(keys, variable.Key)
			values[variable.Key] = variable.Value
		}
	}
	sort.Strings(keys)
	args := []string{}
	for _, key := range keys {
		args = append(args, "--build-arg", key+"="+values[key])
	}
	return args
}

func (j *job) dockerBuild(ctx context.Context, dockerfile string) (string, error) {
	image := j.imageTag()
	args := []string{"docker", "build", "--progress", "plain", "--tag", image, "--file", dockerfile,
		"--label", docker.LabelManaged + "=true", "--label", docker.LabelID + "=" + j.app.ID}
	buildArgs := j.buildArgs()
	args = append(args, buildArgs...)
	args = append(args, j.contextDir())

	display := fmt.Sprintf("docker build --tag %s --file %s", image, path.Base(dockerfile))
	if len(buildArgs) > 0 {
		display += fmt.Sprintf(" (+%d build args)", len(buildArgs)/2)
	}
	j.log.Command(display)
	if err := j.ex.Stream(ctx, shell.Join(args...), j.log.Output); err != nil {
		return "", fmt.Errorf("docker build falhou: %w", err)
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
	return fmt.Sprintf(`FROM %s
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
COPY %s
RUN rm -rf /usr/share/nginx/html/.git
`, staticBaseImage, fallback, copyArgs), nil
}

// nixpacksBuilder detects the stack and builds without a Dockerfile. It needs
// the nixpacks CLI installed on the server.
type nixpacksBuilder struct{}

func (nixpacksBuilder) build(ctx context.Context, j *job) (string, error) {
	if _, err := j.ex.Run(ctx, "command -v nixpacks"); err != nil {
		return "", fmt.Errorf("o Nixpacks não está instalado no servidor. Instale com " +
			"`curl -sSL https://nixpacks.com/install.sh | bash` ou use o build pack Dockerfile")
	}
	image := j.imageTag()
	config := j.app.Config.Build
	args := []string{"nixpacks", "build", j.contextDir(), "--name", image,
		"--label", docker.LabelManaged + "=true", "--label", docker.LabelID + "=" + j.app.ID}
	for flag, value := range map[string]string{
		"--install-cmd": config.InstallCommand, "--build-cmd": config.BuildCommand, "--start-cmd": config.StartCommand,
	} {
		if value != "" {
			args = append(args, flag, value)
		}
	}
	for _, variable := range j.env {
		args = append(args, "--env", variable.Key+"="+variable.Value)
	}
	j.log.Command("nixpacks build --name " + image)
	if err := j.ex.Stream(ctx, shell.Join(args...), j.log.Output); err != nil {
		return "", fmt.Errorf("nixpacks build falhou: %w", err)
	}
	return image, nil
}

// imageBuilder pulls a ready-made image instead of building.
type imageBuilder struct{}

func (imageBuilder) build(ctx context.Context, j *job) (string, error) {
	image := j.app.Config.Source.Image
	j.log.Command("docker pull " + image)
	if err := j.ex.Stream(ctx, shell.Join("docker", "pull", image), j.log.Output); err != nil {
		// A registry outage or rate limit must not block a deploy whose image
		// is already on the server.
		if ctx.Err() != nil || !docker.ImageExists(ctx, j.ex, image) {
			return "", fmt.Errorf("docker pull falhou: %w", err)
		}
		j.log.Info("Não foi possível atualizar a imagem no registry; usando a cópia local.")
	}
	return image, nil
}
