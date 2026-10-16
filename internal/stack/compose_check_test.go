package stack

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/envvar"
)

// TestEveryTemplateIsAcceptedByDockerCompose hands every template of the
// catalog, rendered as a deployment would render it, to `docker compose
// config`: the parser and schema of docker itself, not ours. It pulls and
// starts nothing. It needs docker, so it only runs when asked:
//
//	PIER_COMPOSE_CHECK=1 go test ./internal/stack -run AcceptedByDockerCompose
func TestEveryTemplateIsAcceptedByDockerCompose(t *testing.T) {
	if os.Getenv("PIER_COMPOSE_CHECK") == "" {
		t.Skip("set PIER_COMPOSE_CHECK=1 to validate the catalog with docker compose")
	}
	templates, err := Templates()
	if err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, template := range templates {
		routes := []Route{}
		for _, route := range template.Routes {
			routes = append(routes, Route{Service: route.Service, Port: route.Port, Domain: "https://" + template.Slug + ".example.com" + route.Path})
		}
		candidate := Stack{ID: "check" + strings.ReplaceAll(template.Slug, "-", ""), Compose: template.Compose, Routes: routes}
		rendered, files, err := render(candidate)
		if err != nil {
			t.Errorf("%s: render: %v", template.Slug, err)
			continue
		}
		variables := generatedDefaults(template.Compose, nil)
		for key, value := range routeVariables(routes) {
			variables[key] = value
		}
		variables[directoryVariable] = "/data/pier/stacks/" + candidate.ID
		// What only the person can provide gets a placeholder.
		for _, variable := range template.Variables {
			if variable.Required {
				variables[variable.Name] = "placeholder"
			}
		}
		dir := t.TempDir()
		for _, file := range files {
			target := filepath.Join(dir, file.Path)
			_ = os.MkdirAll(filepath.Dir(target), 0o755)
			_ = os.WriteFile(target, file.Content, file.Mode)
		}
		_ = os.WriteFile(filepath.Join(dir, "docker-compose.yaml"), rendered, 0o600)
		_ = os.WriteFile(filepath.Join(dir, ".env"), envvar.FormatDotenv(variables), 0o600)
		command := exec.Command("docker", "compose", "--project-name", candidate.ID, "--file", filepath.Join(dir, "docker-compose.yaml"),
			"--env-file", filepath.Join(dir, ".env"), "config", "--quiet")
		if output, err := command.CombinedOutput(); err != nil {
			failures++
			t.Errorf("%s: docker compose rejects it: %s", template.Slug, lastLines(string(output), 2))
		}
	}
	t.Logf("%d templates checked with docker compose, %d rejected", len(templates), failures)
}

func lastLines(text string, count int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}
	return strings.Join(lines, " | ")
}
