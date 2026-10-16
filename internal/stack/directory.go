package stack

import (
	"context"
	"io"
	"path"
	"regexp"
	"strings"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

// A directory stack runs the compose file that lives in a folder of the
// server. Pier never copies that file nor what it refers to: the env files,
// secret files and bind mounts stay in the folder, and docker compose reads
// them from there. What Pier adds (labels, the ingress network) goes into an
// override file kept in its own directory.

// StandardComposeFiles are the names docker compose looks for, in its order.
var StandardComposeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

const (
	// maxComposeBytes bounds what is read from a file chosen by the user.
	maxComposeBytes = 1 << 20
	dotenvFileName  = ".env"
)

var (
	composeFilePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+(/[A-Za-z0-9._-]+)*\.ya?ml$`)
	projectNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
)

// cleanDirectory validates a folder path typed or picked by the user.
func cleanDirectory(raw string) (string, bool) {
	if !path.IsAbs(raw) || strings.ContainsAny(raw, "\x00\n\r") {
		return "", false
	}
	cleaned := path.Clean(raw)
	return cleaned, cleaned != "/"
}

func within(parent, child string) bool {
	return child == parent || strings.HasPrefix(child, strings.TrimRight(parent, "/")+"/")
}

func exists(ctx context.Context, ex executor.Executor, flag, target string) bool {
	_, err := ex.Run(ctx, shell.Join("test", flag, target))
	return err == nil
}

// directorySource validates the folder of a stack on its server and returns
// the compose file found there. Problems the user can fix are reported on
// fields (ok is false); an unreachable server is an error.
func (s *Service) directorySource(ctx context.Context, stack *Stack, fields apperr.Fields) (raw string, ok bool, err error) {
	directory, valid := cleanDirectory(stack.Directory)
	if !valid {
		fields.Add("directory", "Informe o caminho absoluto de uma pasta do servidor (ex.: /home/deploy/app).")
		return "", false, nil
	}
	stack.Directory = directory
	if stack.ComposeFile != "" && (!composeFilePattern.MatchString(stack.ComposeFile) || strings.Contains(stack.ComposeFile, "..")) {
		fields.Add("compose_file", "Use o nome de um arquivo .yaml ou .yml dentro da pasta.")
		return "", false, nil
	}
	if stack.ServerID == "" {
		return "", false, nil
	}
	ex, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return "", false, err
	}
	if within(target.DataDir, directory) {
		fields.Add("directory", "Esta pasta pertence aos dados do Pier; escolha a pasta do seu projeto.")
		return "", false, nil
	}
	if !exists(ctx, ex, "-d", directory) {
		fields.Add("directory", "A pasta %s não existe no servidor ou não pode ser lida.", directory)
		return "", false, nil
	}
	if stack.ComposeFile == "" {
		for _, candidate := range StandardComposeFiles {
			if exists(ctx, ex, "-f", path.Join(directory, candidate)) {
				stack.ComposeFile = candidate
				break
			}
		}
		if stack.ComposeFile == "" {
			fields.Add("directory", "Nenhum arquivo compose nesta pasta (procurei %s).", strings.Join(StandardComposeFiles, ", "))
			return "", false, nil
		}
	}
	raw, err = readSource(ctx, ex, path.Join(directory, stack.ComposeFile))
	if err != nil {
		fields.Add("compose_file", "Não foi possível ler %s: %s.", stack.ComposeFile, lastLine(err))
		return "", false, nil
	}
	return raw, true, nil
}

// readSource reads a compose file from the server, refusing absurd sizes.
func readSource(ctx context.Context, ex executor.Executor, file string) (string, error) {
	reader, err := ex.ReadFile(ctx, file)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maxComposeBytes+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxComposeBytes {
		return "", apperr.Invalid("O arquivo compose passa de 1 MB.")
	}
	return string(content), nil
}

func lastLine(err error) string {
	lines := strings.Split(strings.TrimSpace(err.Error()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// projectFor describes how docker compose runs a stack on its server.
func projectFor(ctx context.Context, ex executor.Executor, target server.Server, stack Stack) compose.Project {
	project := compose.Project{Name: stack.composeProject(), Dir: target.ResourceDir(docker.TypeStack, stack.ID)}
	if !stack.FromDirectory() {
		return project
	}
	project.BaseFile = path.Join(stack.Directory, stack.ComposeFile)
	// docker compose only reads the .env of the folder by default; since Pier
	// passes its own env file, the one of the folder must be named too.
	if dotenv := path.Join(stack.Directory, dotenvFileName); exists(ctx, ex, "-f", dotenv) {
		project.EnvFiles = []string{dotenv}
	}
	return project
}

// renderOverride builds the override file of a directory stack: the labels,
// networks and restart policy Pier adds, for the services docker compose
// resolved from the folder. doc is the main compose file parsed loosely; a
// service that comes from an include is not in it and gets the labels only.
func renderOverride(stack Stack, doc document, services []string) ([]byte, error) {
	declared, _ := doc["services"].(map[string]any)
	known := map[string]bool{}
	overrides := map[string]any{}
	for _, name := range services {
		known[name] = true
		definition, _ := declared[name].(map[string]any)
		result, err := publish(stack, name, definition["networks"], hasKey(definition, "network_mode"))
		if err != nil {
			return nil, err
		}
		labels := map[string]any{}
		for key, value := range result.labels {
			labels[key] = value
		}
		entry := map[string]any{"labels": labels}
		if result.networks != nil {
			entry["networks"] = result.networks
		}
		if definition != nil && !hasKey(definition, "restart") {
			entry["restart"] = "unless-stopped"
		}
		overrides[name] = entry
	}
	for _, route := range stack.Routes {
		if !known[route.Service] {
			return nil, apperr.Invalid("O domínio %s aponta para o serviço %q, que não existe no compose da pasta.",
				route.Domain, route.Service)
		}
	}
	return marshalCompose(map[string]any{"services": overrides, "networks": withIngress(nil, stack.ID)})
}
