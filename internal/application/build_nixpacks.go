package application

import (
	"context"
	"encoding/json"
	"io"
	"path"
	"strings"

	"pier/internal/envvar"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

func (j *job) nixpacksBuild(ctx context.Context) (string, error) {
	config := j.app.Config.Build
	args := []string{"nixpacks", "build", j.contextDir(), "--out", j.contextDir()}
	planArgs := []string{"nixpacks", "plan", j.contextDir(), "--format", "json"}
	for _, option := range [][2]string{{"--install-cmd", config.InstallCommand}, {"--build-cmd", config.BuildCommand}, {"--start-cmd", config.StartCommand}} {
		if option[1] != "" {
			args = append(args, option[0], option[1])
			planArgs = append(planArgs, option[0], option[1])
		}
	}
	plain := *j
	plain.env = nil
	secrets := []string{}
	for _, variable := range j.buildTimeVariables() {
		if variable.SecretBuild {
			secrets = append(secrets, variable.Key)
			continue
		}
		plain.env = append(plain.env, variable)
		args = append(args, "--env", variable.Key)
		planArgs = append(planArgs, "--env", variable.Key)
	}
	// Start with a new asset directory. Repository-controlled links must not
	// redirect Nixpacks' generated output, even to another path inside src.
	assetDir := path.Join(j.contextDir(), ".nixpacks")
	if _, err := j.ex.Run(ctx, shell.Join("rm", "-rf", "--", assetDir)); err != nil {
		return "", err
	}
	var plan strings.Builder
	if err := plain.streamBuildEnvironment(ctx, planArgs, func(line string) { plan.WriteString(line); plan.WriteByte('\n') }); err != nil {
		return "", i18n.Errorf("nixpacks build falhou: %w", err)
	}
	var generated struct {
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal([]byte(plan.String()), &generated); err != nil {
		return "", i18n.Errorf("o Nixpacks não informou um plano de build válido: %w", err)
	}
	j.log.Command("nixpacks build --out " + j.contextDir())
	if err := plain.streamBuildEnvironment(ctx, args, j.log.Output); err != nil {
		return "", i18n.Errorf("nixpacks build falhou: %w", err)
	}
	// Nixpacks currently has no CLI secret option. Its generated Dockerfile is
	// built by Pier, using BuildKit secret env mounts on RUN instructions only.
	dockerfile := path.Join(assetDir, "Dockerfile")
	reader, err := j.ex.ReadFile(ctx, dockerfile)
	if err != nil {
		return "", err
	}
	content, readErr := io.ReadAll(io.LimitReader(reader, (4<<20)+1))
	closeErr := reader.Close()
	if readErr != nil {
		return "", readErr
	}
	if closeErr != nil {
		return "", closeErr
	}
	if len(content) > 4<<20 {
		return "", i18n.Errorf("o Dockerfile gerado pelo Nixpacks ultrapassa o limite de tamanho")
	}
	if len(secrets) > 0 {
		content = []byte(nixpacksSecretMounts(string(content), secrets))
		if err := j.ex.WriteFile(ctx, dockerfile, content, 0o600); err != nil {
			return "", err
		}
	}
	// Never copy Nixpacks' helper script (which includes plain build values)
	// into an image layer; Pier invokes BuildKit itself.
	if err := removeBuildCredentials(ctx, j.ex, path.Join(assetDir, "build.sh")); err != nil {
		return "", err
	}
	buildJob := *j
	buildJob.env = append([]envvar.Variable(nil), j.env...)
	existing := map[string]bool{}
	for _, variable := range j.env {
		existing[variable.Key] = true
	}
	for key, value := range generated.Variables {
		if !envvar.ValidKey(key) {
			return "", i18n.Errorf("nome de variável inválido no plano do Nixpacks: %s", key)
		}
		if !existing[key] {
			buildJob.env = append(buildJob.env, envvar.Variable{Key: key, Value: value, BuildTime: true})
		}
	}
	return buildJob.dockerBuild(ctx, dockerfile)
}

// Nixpacks emits one-line ARG/ENV lists and shell-form RUN instructions.
// env secret mounts require the Dockerfile frontend 1.10 or newer.
func nixpacksSecretMounts(dockerfile string, secrets []string) string {
	secret := map[string]bool{}
	mounts := ""
	for _, key := range secrets {
		secret[key] = true
		mounts += " --mount=type=secret,id=" + key + ",env=" + key
	}
	lines := strings.Split(dockerfile, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# syntax=") {
			lines[i] = ""
			continue
		}
		if strings.HasPrefix(trimmed, "RUN ") {
			lines[i] = "RUN" + mounts + " " + strings.TrimPrefix(trimmed, "RUN ")
			continue
		}
		for _, instruction := range []string{"ARG", "ENV"} {
			if !strings.HasPrefix(trimmed, instruction+" ") {
				continue
			}
			kept := []string{}
			for _, token := range strings.Fields(strings.TrimPrefix(trimmed, instruction+" ")) {
				key, _, _ := strings.Cut(token, "=")
				if !secret[key] {
					kept = append(kept, token)
				}
			}
			if len(kept) == 0 {
				lines[i] = ""
			} else {
				lines[i] = instruction + " " + strings.Join(kept, " ")
			}
		}
	}
	return "# syntax=docker/dockerfile:1.10\n" + strings.Join(lines, "\n")
}
