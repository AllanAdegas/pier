package application

import (
	"context"
	"errors"
	"path"
	"strings"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// streamBuildEnvironment keeps values out of both the shell's and the CLI's
// argv. Only variable names reach --build-arg, --env and --secret.
func (j *job) streamBuildEnvironment(ctx context.Context, args []string, sink executor.LineSink) (err error) {
	filename := path.Join(j.server.BuildDir(j.deployment.ID), "build.env.sh")
	var content strings.Builder
	for _, variable := range j.buildTimeVariables() {
		if strings.ContainsRune(variable.Value, '\x00') {
			return i18n.Errorf("o valor de build de %s contém um caractere nulo inválido", variable.Key)
		}
		// Docker's client configuration must remain controlled by Pier. Otherwise
		// e.g. DOCKER_HOST would redirect the build (and its secrets) elsewhere.
		if reservedBuildEnvironment(variable.Key) {
			return i18n.Errorf("a variável %s é reservada pelo executor de build; renomeie a variável", variable.Key)
		}
		content.WriteString("export " + variable.Key + "=" + shell.Quote(variable.Value) + "\n")
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		err = errors.Join(err, removeBuildCredentials(cleanupCtx, j.ex, filename))
	}()
	if err := j.ex.WriteFile(ctx, filename, []byte(content.String()), 0o600); err != nil {
		return err
	}
	command := ". " + shell.Quote(filename) + " && " + shell.Join(args...)
	return j.ex.Stream(ctx, command, sink)
}

func reservedBuildEnvironment(key string) bool {
	return key == "PATH" || key == "HOME" || key == "ENV" || key == "BASH_ENV" || key == "SHELLOPTS" || key == "CDPATH" || key == "IFS" || key == "XDG_CONFIG_HOME" || key == "SSH_AUTH_SOCK" || strings.HasPrefix(key, "LD_") || strings.HasPrefix(key, "DYLD_") || strings.HasPrefix(key, "DOCKER_") || strings.HasPrefix(key, "BUILDX_") || strings.HasPrefix(key, "BUILDKIT_")
}
