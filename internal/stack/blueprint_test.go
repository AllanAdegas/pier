package stack

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func blueprintTemplate(t *testing.T, source string) (Template, error) {
	t.Helper()
	var template Template
	if err := yaml.Unmarshal([]byte(source), &template); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	return template, expandBlueprint(&template)
}

func expanded(t *testing.T, source string) (Template, document) {
	t.Helper()
	template, err := blueprintTemplate(t, source)
	if err != nil {
		t.Fatalf("expandBlueprint: %v", err)
	}
	doc, err := parseCompose(template.Compose)
	if err != nil {
		t.Fatalf("generated compose does not parse: %v\n%s", err, template.Compose)
	}
	if err := CheckTemplate(template); err != nil {
		t.Fatalf("generated template does not check: %v\n%s", err, template.Compose)
	}
	return template, doc
}

func serviceOf(doc document, name string) map[string]any {
	services, _ := doc["services"].(map[string]any)
	service, _ := services[name].(map[string]any)
	return service
}

func TestABlueprintBecomesAComposeFileWithItsRoute(t *testing.T) {
	template, doc := expanded(t, `
name: Notes
services:
  web:
    image: example/notes:1.2.3
    port: 8080
    data: /var/lib/notes
    env:
      MODE: production
      DATABASE_URL: "{{postgres.url}}"
    health: wget:/health
  worker:
    image: example/notes:1.2.3
    command: [notes, worker]
    env:
      DATABASE_URL: "{{postgres.url}}"
with:
  postgres: {database: notes}
`)
	if len(template.Routes) != 1 || template.Routes[0] != (TemplateRoute{Service: "web", Port: 8080}) {
		t.Fatalf("routes: %+v", template.Routes)
	}
	web := serviceOf(doc, "web")
	environment := web["environment"].([]any)
	if environment[0] != "MODE=production" ||
		environment[1] != "DATABASE_URL=postgresql://${PIER_USER_POSTGRES}:${PIER_PASSWORD_POSTGRES}@postgres:5432/notes" {
		t.Fatalf("environment: %v", environment)
	}
	if web["volumes"].([]any)[0] != "web-data:/var/lib/notes" {
		t.Fatalf("volumes: %v", web["volumes"])
	}
	health := web["healthcheck"].(map[string]any)["test"].([]any)
	if !strings.Contains(health[1].(string), "http://127.0.0.1:8080/health") {
		t.Fatalf("health: %v", health)
	}
	for _, name := range []string{"web", "worker"} {
		depends := serviceOf(doc, name)["depends_on"].(map[string]any)
		if depends["postgres"].(map[string]any)["condition"] != "service_healthy" {
			t.Fatalf("%s must wait for a healthy database: %v", name, depends)
		}
	}
	postgres := serviceOf(doc, "postgres")
	if postgres["image"] != "postgres:17-alpine" || !strings.Contains(template.Compose, "POSTGRES_DB=notes") {
		t.Fatalf("postgres: %v", postgres)
	}
	if strings.Index(template.Compose, "web:") > strings.Index(template.Compose, "postgres:") {
		t.Error("application services come before the components")
	}
}

func TestComponentsCoverTheUsualOptions(t *testing.T) {
	_, doc := expanded(t, `
name: Files
services:
  app:
    image: example/files:2.0.0
    port: 3000
    env:
      REDIS_URL: "{{cache.url}}"
      S3_ENDPOINT: "{{minio.endpoint}}"
      SEARCH_DB: "{{vectors.url}}"
with:
  cache: {kind: valkey, password: true}
  minio: {buckets: [uploads, avatars]}
  vectors: {kind: postgres, flavor: pgvector, version: "18"}
`)
	cache := serviceOf(doc, "cache")
	if strings.Contains(strings.Join(anyStrings(cache["command"]), " "), "PIER_PASSWORD") {
		t.Errorf("the cache password must come from the environment, not the command: %v", cache["command"])
	}
	init := serviceOf(doc, "minio-init")
	if !strings.Contains(strings.Join(anyStrings(init["entrypoint"]), " "), "store/avatars && mc mb --ignore-existing store/uploads") {
		t.Errorf("buckets: %v", init["entrypoint"])
	}
	depends := serviceOf(doc, "app")["depends_on"].(map[string]any)
	if depends["minio-init"].(map[string]any)["condition"] != "service_completed_successfully" {
		t.Errorf("the app waits for the buckets: %v", depends)
	}
	vectors := serviceOf(doc, "vectors")
	if vectors["image"] != "pgvector/pgvector:pg18" || vectors["volumes"].([]any)[0] != "vectors-data:/var/lib/postgresql" {
		t.Errorf("postgres 18 keeps its data under /var/lib/postgresql: %v", vectors)
	}
	if !strings.Contains(serviceOf(doc, "app")["environment"].([]any)[0].(string), "redis://:${PIER_PASSWORD_CACHE}@cache:6379") {
		t.Errorf("cache url: %v", serviceOf(doc, "app")["environment"])
	}
}

func TestComponentOptionsReachTheService(t *testing.T) {
	_, doc := expanded(t, `
name: Tuned
services:
  app:
    image: example/app:1.0.0
with:
  db: {kind: postgres, args: ["-c", "wal_level=logical"], env: {TZ: UTC}, shm_size: 1g, user: app}
  cache: {kind: redis, args: ["--maxmemory", "200mb"]}
  locked: {kind: valkey, password: true, args: ["--maxmemory", "64mb"]}
`)
	db := serviceOf(doc, "db")
	if strings.Join(anyStrings(db["command"]), " ") != "postgres -c wal_level=logical" || db["shm_size"] != "1g" {
		t.Errorf("postgres options: %v", db)
	}
	if !strings.Contains(strings.Join(anyStrings(db["environment"]), " "), "TZ=UTC") ||
		!strings.Contains(strings.Join(anyStrings(db["environment"]), " "), "POSTGRES_USER=app") {
		t.Errorf("postgres environment: %v", db["environment"])
	}
	if strings.Join(anyStrings(serviceOf(doc, "cache")["command"]), " ") != "redis-server --appendonly yes --maxmemory 200mb" {
		t.Errorf("redis args: %v", serviceOf(doc, "cache")["command"])
	}
	if !strings.HasSuffix(anyStrings(serviceOf(doc, "locked")["command"])[2], "--maxmemory 64mb") {
		t.Errorf("valkey args with password: %v", serviceOf(doc, "locked")["command"])
	}
}

func TestPrivateServicesAndExplicitNeedsAreRespected(t *testing.T) {
	template, doc := expanded(t, `
name: Two
services:
  api:
    image: example/api:1.0.0
    port: 9000
    path: /api
  admin:
    image: example/admin:1.0.0
    port: 9001
    private: true
    needs: [api]
with:
  redis: {}
`)
	if len(template.Routes) != 1 || template.Routes[0].Path != "/api" {
		t.Fatalf("routes: %+v", template.Routes)
	}
	depends := serviceOf(doc, "admin")["depends_on"].(map[string]any)
	if len(depends) != 1 || depends["api"].(map[string]any)["condition"] != "service_started" {
		t.Fatalf("needs: %v", depends)
	}
}

func TestAliasesAreNamesOnTheStackNetwork(t *testing.T) {
	_, doc := expanded(t, `
name: Chat
services:
  xmpp:
    image: example/xmpp:1.0.0
    aliases: [xmpp.chat.local, auth.chat.local]
`)
	networks := serviceOf(doc, "xmpp")["networks"].(map[string]any)
	aliases := networks["default"].(map[string]any)["aliases"].([]any)
	if len(aliases) != 2 || aliases[1] != "auth.chat.local" {
		t.Fatalf("aliases: %v", networks)
	}
}

func TestBlueprintMistakesAreRefused(t *testing.T) {
	cases := map[string]string{
		"unknown component":   "services: {a: {image: x/y:1}}\nwith: {cache: {}}",
		"unknown field":       "services: {a: {image: x/y:1, env: {U: '{{postgres.nope}}'}}}\nwith: {postgres: {}}",
		"not a component":     "services: {a: {image: x/y:1, env: {U: '{{mysql.url}}'}}}\nwith: {postgres: {}}",
		"bad version":         "services: {a: {image: x/y:1}}\nwith: {postgres: {version: '9'}}",
		"both forms":          "compose: 'services: {a: {image: x/y:1}}'\nservices: {a: {image: x/y:1}}",
		"unknown need":        "services: {a: {image: x/y:1, needs: [b]}}",
		"name clash":          "services: {postgres: {image: x/y:1}}\nwith: {postgres: {}}",
		"health without port": "services: {a: {image: x/y:1, health: tcp}}",
		"environment key":     "services: {a: {image: x/y:1, environment: [A=1]}}",
	}
	for name, source := range cases {
		if _, err := blueprintTemplate(t, source); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func anyStrings(value any) []string {
	list, _ := value.([]any)
	out := []string{}
	for _, item := range list {
		out = append(out, item.(string))
	}
	return out
}
