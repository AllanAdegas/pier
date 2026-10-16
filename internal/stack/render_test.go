package stack

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"pier/internal/platform/apperr"
)

func TestEveryTemplateParsesAndRenders(t *testing.T) {
	templates, err := Templates()
	if err != nil {
		t.Fatalf("Templates: %v", err)
	}
	if len(templates) == 0 {
		t.Fatal("the catalog is empty")
	}
	for _, template := range templates {
		t.Run(template.Slug, func(t *testing.T) {
			if err := CheckTemplate(template); err != nil {
				t.Fatal(err)
			}
			if template.Name == "" || template.Description == "" || template.Category == "" {
				t.Errorf("incomplete metadata: %+v", template.Summary())
			}
		})
	}
}

func TestRenderPublishesOnlyRoutedServices(t *testing.T) {
	candidate := Stack{
		ID: "stk1",
		Compose: `
services:
  web:
    image: nginx
    labels:
      - "custom=1"
  db:
    image: postgres
    networks: [internal]
networks:
  internal:
`,
		Routes: []Route{{Service: "web", Port: 80, Domain: "https://web.example.com"}},
	}
	rendered, _, err := render(candidate)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc struct {
		Services map[string]struct {
			Labels   map[string]string `yaml:"labels"`
			Networks any               `yaml:"networks"`
			Restart  string            `yaml:"restart"`
		} `yaml:"services"`
		Networks map[string]map[string]any `yaml:"networks"`
	}
	if err := yaml.Unmarshal(rendered, &doc); err != nil {
		t.Fatalf("rendered compose is not valid YAML: %v", err)
	}
	web, db := doc.Services["web"], doc.Services["db"]
	if web.Labels["custom"] != "1" || web.Labels["pier.id"] != "stk1" || web.Labels["traefik.enable"] != "true" {
		t.Errorf("web labels were not merged correctly: %v", web.Labels)
	}
	if web.Labels["traefik.docker.network"] != "stk1_ingress" {
		t.Errorf("web must be reached through the ingress network, got %q", web.Labels["traefik.docker.network"])
	}
	webNetworks, _ := web.Networks.(map[string]any)
	if _, joined := webNetworks[ingressKey]; !joined {
		t.Errorf("routed service must join the ingress network: %v", web.Networks)
	}
	if _, keptDefault := webNetworks["default"]; !keptDefault {
		t.Errorf("routed service must keep its default network: %v", web.Networks)
	}
	// The unrouted service keeps exactly the networks the user declared.
	if dbNetworks, _ := db.Networks.([]any); len(dbNetworks) != 1 || dbNetworks[0] != "internal" {
		t.Errorf("unrouted service networks must stay untouched: %v", db.Networks)
	}
	if db.Labels["traefik.enable"] != "" || db.Labels["pier.managed"] != "true" {
		t.Errorf("unexpected db labels: %v", db.Labels)
	}
	if db.Restart != "unless-stopped" {
		t.Errorf("restart policy default missing: %q", db.Restart)
	}
	if doc.Networks[ingressKey]["name"] != "stk1_ingress" {
		t.Errorf("ingress network not declared: %v", doc.Networks)
	}
}

func TestGeneratedDefaultsAreStableKeysWithFreshValues(t *testing.T) {
	raw := "a: ${PIER_PASSWORD_DB}\nb: ${PIER_PASSWORD_DB}\nc: $PIER_SECRET_APP\nd: ${PIER_USER_ADMIN:-x}\ne: ${PIER_URL_WEB}"
	defaults := generatedDefaults(raw, nil)
	for _, key := range []string{"PIER_PASSWORD_DB", "PIER_SECRET_APP", "PIER_USER_ADMIN"} {
		if defaults[key] == "" {
			t.Errorf("missing generated value for %s", key)
		}
	}
	if _, generated := defaults["PIER_URL_WEB"]; generated {
		t.Error("route variables must not be generated as secrets")
	}
	if len(defaults) != 3 {
		t.Errorf("got %d variables, want 3: %v", len(defaults), defaults)
	}
	if !strings.HasPrefix(defaults["PIER_USER_ADMIN"], "u") {
		t.Errorf("generated user should start with a letter: %q", defaults["PIER_USER_ADMIN"])
	}
}

func TestRejectsRouteToUnknownService(t *testing.T) {
	candidate := Stack{
		Name: "x", Compose: "services:\n  web:\n    image: nginx\n",
		Routes: []Route{{Service: "api", Port: 80, Domain: "https://x.example.com"}},
	}
	if err := validate(&candidate, candidate.Compose, true, apperr.Fields{}); err == nil {
		t.Fatal("expected a validation error for a route to a missing service")
	}
}
