package stack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestGeneratedValuesFollowTheVocabulary(t *testing.T) {
	raw := strings.Join([]string{
		"${PIER_PASSWORD_DB}", "$PIER_PASSWORD_64_APIKEY", "${PIER_PASSWORD_2FA}", "${PIER_USER_ADMIN}",
		"${PIER_SECRET_APP}", "${PIER_HEX_TOKEN}", "${PIER_HEX_64_KEY}", "${PIER_BASE64_KEY}", "${PIER_BASE64_64_SALT}",
		"${CAIS_PASSWORD_OLD}", "${PIER_USERNAME}", "${PIER_URL_APP}", "${PIER_HOST_APP}", "${SERVICE_PASSWORD_X}",
		"${OPENAI_API_KEY}",
	}, "\n")
	values := generatedDefaults(raw, nil)
	expected := map[string]string{
		"PIER_PASSWORD_DB":        `^[A-Za-z0-9]{32}$`,
		"PIER_PASSWORD_64_APIKEY": `^[A-Za-z0-9]{64}$`,
		// A name that merely starts with digits is not a size.
		"PIER_PASSWORD_2FA":   `^[A-Za-z0-9]{32}$`,
		"PIER_USER_ADMIN":     `^u[a-z0-9]{11}$`,
		"PIER_SECRET_APP":     `^[0-9a-f]{64}$`,
		"PIER_HEX_TOKEN":      `^[0-9a-f]{32}$`,
		"PIER_HEX_64_KEY":     `^[0-9a-f]{64}$`,
		"PIER_BASE64_KEY":     `^[A-Za-z0-9+/]{43}=$`,
		"PIER_BASE64_64_SALT": `^[A-Za-z0-9+/]{86}==$`,
		"CAIS_PASSWORD_OLD":   `^[A-Za-z0-9]{32}$`,
	}
	for name, pattern := range expected {
		if !regexp.MustCompile(pattern).MatchString(values[name]) {
			t.Errorf("%s = %q does not match %s", name, values[name], pattern)
		}
	}
	// Addresses, look-alikes and names of other platforms are not generated.
	for _, name := range []string{"PIER_USERNAME", "PIER_URL_APP", "PIER_HOST_APP", "SERVICE_PASSWORD_X", "OPENAI_API_KEY"} {
		if _, generated := values[name]; generated {
			t.Errorf("%s must not be generated", name)
		}
	}
	if len(values) != len(expected) {
		t.Errorf("generated %d values, want %d: %v", len(values), len(expected), values)
	}
}

// Regenerating the password of an initialized database would lock the stack
// out of it.
func TestGeneratedValuesAreNeverRegenerated(t *testing.T) {
	existing := map[string]string{"PIER_PASSWORD_DB": "kept", "PIER_SECRET_APP": "kept-too"}
	values := generatedDefaults("${PIER_PASSWORD_DB} ${PIER_SECRET_APP} ${PIER_PASSWORD_NEW}", existing)
	if len(values) != 1 || values["PIER_PASSWORD_NEW"] == "" {
		t.Errorf("only the missing variable must be created: %v", values)
	}
}

func TestSupabaseTokensAreSignedWithTheStoredKey(t *testing.T) {
	raw := "${PIER_SUPABASEANON_KEY} ${PIER_SUPABASESERVICE_KEY} ${PIER_PASSWORD_JWT}"
	fresh := generatedDefaults(raw, nil)
	// When the key already exists, new tokens must be signed with it, not
	// with a key that will never be stored.
	stored := generatedDefaults(raw, map[string]string{signingKeyVariable: "the-stored-signing-key"})
	if _, regenerated := stored[signingKeyVariable]; regenerated {
		t.Fatal("the stored signing key must be kept")
	}
	cases := []struct {
		values     map[string]string
		signingKey string
	}{{fresh, fresh[signingKeyVariable]}, {stored, "the-stored-signing-key"}}
	for _, item := range cases {
		for name, role := range map[string]string{"PIER_SUPABASEANON_KEY": "anon", "PIER_SUPABASESERVICE_KEY": "service_role"} {
			parts := strings.Split(item.values[name], ".")
			if len(parts) != 3 {
				t.Fatalf("%s is not a JWT: %q", name, item.values[name])
			}
			mac := hmac.New(sha256.New, []byte(item.signingKey))
			mac.Write([]byte(parts[0] + "." + parts[1]))
			if base64.RawURLEncoding.EncodeToString(mac.Sum(nil)) != parts[2] {
				t.Errorf("%s is not signed with the signing key", name)
			}
			var claims map[string]any
			payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
			_ = json.Unmarshal(payload, &claims)
			lifetime := claims["exp"].(float64) - claims["iat"].(float64)
			if claims["role"] != role || claims["iss"] != "supabase" || lifetime < 99*365*24*3600 {
				t.Errorf("%s has wrong claims: %v", name, claims)
			}
		}
	}
}

func TestPublishedAddressesReachTheComposeFile(t *testing.T) {
	raw := `
services:
  app:
    image: example/app
    environment:
      - 'PUBLIC_URL=${PIER_URL_APP}'
      - 'HOST=${PIER_HOST_APP}'
      - 'STORAGE=${PIER_URL_MINIO}'
      - 'WORKER=${PIER_URL_MY_WORKER}'
  minio:
    image: pgsty/minio
  my-worker:
    image: example/worker
`
	routes := []Route{
		{Service: "app", Port: 3000, Domain: "https://app.example.com"},
		{Service: "app", Port: 3000, Domain: "https://alias.example.com"},
		{Service: "minio", Port: 9000, Domain: "https://files.example.com/s3"},
	}
	variables := routeVariables(routes)
	expected := map[string]string{
		// The first domain of a service is its address; the others are aliases.
		"PIER_URL_APP":    "https://app.example.com",
		"PIER_HOST_APP":   "app.example.com",
		"PIER_URL_MINIO":  "https://files.example.com/s3",
		"PIER_HOST_MINIO": "files.example.com",
	}
	for name, want := range expected {
		if variables[name] != want {
			t.Errorf("%s = %q, want %q", name, variables[name], want)
		}
	}
	if pending := unpublished(raw, variables); len(pending) != 1 || pending[0] != "my_worker" {
		t.Errorf("only the worker has no address: %v", pending)
	}
	if none := routeVariables(nil); len(none) != 0 || len(unpublished(raw, none)) != 3 {
		t.Errorf("without domains nothing is set: %v", none)
	}
}

func TestRenderResolvesWhatDockerComposeDoesNotUnderstand(t *testing.T) {
	candidate := Stack{ID: "stk1", Compose: `
services:
  app:
    image: example/app
    environment:
      - PLAIN=value
    volumes:
      - data:/data
      - type: bind
        source: ./etc/entrypoint.sh
        target: /entrypoint.sh
        mode: '0750'
        content: "#!/bin/sh\nexec \"$@\"\n"
      - type: bind
        source: ./config.ini
        target: /config.ini
        content: "limit = ${LIMIT}\n"
      - type: bind
        source: ./uploads
        target: /uploads
        is_directory: true
      - type: volume
        source: modules
        target: /modules
        is_directory: true
volumes:
  data:
  modules:
`}
	rendered, files, err := render(candidate)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(rendered)
	for _, leftover := range []string{"content:", "is_directory", "mode:"} {
		if strings.Contains(text, leftover) {
			t.Errorf("%q must not reach docker compose:\n%s", leftover, text)
		}
	}
	if len(files) != 2 {
		t.Fatalf("expected the two files with content, got %+v", files)
	}
	byPath := map[string]hostFile{}
	for _, file := range files {
		byPath[file.Path] = file
	}
	// The content is written verbatim: "$@" and "${LIMIT}" belong to the file.
	if script := byPath["etc/entrypoint.sh"]; script.Mode != 0o750 || string(script.Content) != "#!/bin/sh\nexec \"$@\"\n" {
		t.Errorf("entrypoint: %+v %q", script.Mode, script.Content)
	}
	if config := byPath["config.ini"]; config.Mode != 0o644 || string(config.Content) != "limit = ${LIMIT}\n" {
		t.Errorf("config: %+v %q", config.Mode, config.Content)
	}
	var doc struct {
		Services map[string]struct {
			Environment []string         `yaml:"environment"`
			Volumes     []map[string]any `yaml:"volumes"`
		} `yaml:"services"`
	}
	// The short-syntax volume is a string; decode leniently.
	var loose map[string]any
	if err := yaml.Unmarshal(rendered, &loose); err != nil {
		t.Fatal(err)
	}
	_ = doc
	app := loose["services"].(map[string]any)["app"].(map[string]any)
	environment := app["environment"].([]any)
	if len(environment) != 1 || environment[0] != "PLAIN=value" {
		t.Errorf("the environment is passed as it is: %v", environment)
	}
	uploads := app["volumes"].([]any)[3].(map[string]any)
	if bind, _ := uploads["bind"].(map[string]any); bind["create_host_path"] != true {
		t.Errorf("a directory mount asks docker to create it: %v", uploads)
	}
}

func TestInlineContentIsOnlyWrittenInsideTheStackDirectory(t *testing.T) {
	for _, source := range []string{"/etc/cron.d/job", "../outside.sh", "${HOME}/x.sh"} {
		candidate := Stack{ID: "stk1", Compose: "services:\n  app:\n    image: x\n    volumes:\n      - type: bind\n        source: '" +
			source + "'\n        target: /x\n        content: 'echo hi'\n"}
		if _, _, err := render(candidate); err == nil {
			t.Errorf("content for %q must be refused", source)
		}
	}
}

func TestVariablesAreFoundTheWayComposeInterpolatesThem(t *testing.T) {
	doc, err := parseCompose(`
services:
  app:
    image: "example/app:${TAG:-latest}"
    environment:
      - 'KEY=${APP_KEY:?defina APP_KEY}'
      - 'HUB=${HUB_URL:-$PIER_URL_HUB}'
      - 'PASS=${PIER_PASSWORD_DB}'
      - PLAIN=$PLAIN_NAME
      - 'LITERAL=$$NOT_A_VARIABLE'
      - 'OPTIONAL=${EMPTY_OK-}'
    # ${IN_A_COMMENT} is not interpolated
    volumes:
      - type: bind
        source: ./run.sh
        target: /run.sh
        content: 'echo "${INSIDE_A_FILE}"'
`)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]Variable{}
	for _, variable := range doc.referencedVariables() {
		found[variable.Name] = variable
	}
	checks := map[string]Variable{
		"TAG":              {Default: "latest", Kind: VariableUser},
		"APP_KEY":          {Required: true, Kind: VariableUser},
		"HUB_URL":          {Default: "$PIER_URL_HUB", Kind: VariableUser},
		"PIER_URL_HUB":     {Kind: VariableAddress},
		"PIER_PASSWORD_DB": {Kind: VariableGenerated},
		"PLAIN_NAME":       {Kind: VariableUser},
		"EMPTY_OK":         {Kind: VariableUser},
	}
	for name, want := range checks {
		got, ok := found[name]
		if !ok || got.Default != want.Default || got.Required != want.Required || got.Kind != want.Kind {
			t.Errorf("%s = %+v (found: %v), want %+v", name, got, ok, want)
		}
	}
	for _, absent := range []string{"NOT_A_VARIABLE", "IN_A_COMMENT", "INSIDE_A_FILE"} {
		if _, wrongly := found[absent]; wrongly {
			t.Errorf("%s is not interpolated by compose and must not be listed", absent)
		}
	}
	if len(found) != len(checks) {
		t.Errorf("found %d variables, want %d: %v", len(found), len(checks), found)
	}
}

// Hand-written compose files often use named volumes without declaring them,
// and docker compose refuses that. Found by deploying a catalog template for
// real.
func TestRenderDeclaresTheNamedVolumesServicesUse(t *testing.T) {
	rendered, _, err := render(Stack{ID: "stk1", Compose: `
services:
  app:
    image: x
    volumes:
      - 'app-data:/data'
      - cache:/cache:ro
      - ./local:/local
      - /var/run/docker.sock:/var/run/docker.sock
      - '${HOST_DIR:-/srv}:/srv'
      - /anonymous
      - type: volume
        source: modules
        target: /modules
      - type: bind
        source: ./conf
        target: /conf
volumes:
  cache:
    driver: local
`})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var doc struct {
		Volumes map[string]map[string]any `yaml:"volumes"`
	}
	if err := yaml.Unmarshal(rendered, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Volumes) != 3 {
		t.Fatalf("only the three named volumes belong there: %v", doc.Volumes)
	}
	for _, name := range []string{"app-data", "modules"} {
		if _, declared := doc.Volumes[name]; !declared {
			t.Errorf("%s was used but not declared", name)
		}
	}
	if doc.Volumes["cache"]["driver"] != "local" {
		t.Errorf("a declaration the file already has is kept: %v", doc.Volumes["cache"])
	}
}

// Three quirks of real compose files that docker compose rejects; all found
// by handing the whole catalog to "docker compose config".
func TestRenderRepairsWhatDockerComposeRefuses(t *testing.T) {
	source := `
services:
  app:
    image: x
    environment:
      0: 'SALT=${PIER_PASSWORD_SALT}'
      1: PLAIN=1
      PORT: '3000'
      NAMED: value
    volumes:
      - 'memos/:/var/opt/memos'
      - 'named:/data'
    healthcheck:
      test:
        - CMD
        - node
        - '-e'
        - 'fetch(` + "`http://127.0.0.1:${process.env.PORT||4788}/health`" + `)'
        - 'curl ${HOST:-localhost} $${ALREADY_ESCAPED} ${REQUIRED:?defina}'
`
	doc, err := parseCompose(source)
	if err != nil {
		t.Fatal(err)
	}
	if environment, isList := doc["services"].(map[string]any)["app"].(map[string]any)["environment"].([]any); !isList || len(environment) != 4 ||
		environment[0] != "SALT=${PIER_PASSWORD_SALT}" || environment[2] != "NAMED=value" {
		t.Errorf("environment should be a list of 4, positions first: %v", environment)
	}
	rendered, _, err := render(Stack{ID: "stk1", Compose: source})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	text := string(rendered)
	for _, expected := range []string{
		"./memos/:/var/opt/memos",    // a relative path, not a volume called "memos/"
		"named:/data",                // a real volume name is left alone
		"$${process.env.PORT||4788}", // not compose syntax: escaped
		"${HOST:-localhost}",         // compose syntax: untouched
		"$${ALREADY_ESCAPED}",        // already escaped: not escaped twice
		"${REQUIRED:?defina}",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("expected %q in the rendered file:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "$$$") {
		t.Errorf("something was escaped twice:\n%s", text)
	}
}

// Stacks created before the project was renamed from Cais reference
// CAIS_HOST_X, CAIS_URL_X and CAIS_PASSWORD_X. They must keep receiving
// values: found on a real n8n that ended up configured with "localhost".
func TestLegacyNamesFromBeforeTheRenameStillWork(t *testing.T) {
	raw := `
services:
  n8n:
    image: n8n
    environment:
      N8N_HOST: ${CAIS_HOST_N8N:-localhost}
      WEBHOOK_URL: ${CAIS_URL_N8N:-http://localhost:5678}/
      N8N_ENCRYPTION_KEY: ${CAIS_SECRET_ENCRYPTION}
      DB_PASSWORD: ${CAIS_PASSWORD_DB}
`
	doc, err := parseCompose(raw)
	if err != nil {
		t.Fatal(err)
	}
	routes := []Route{
		{Service: "n8n", Port: 5678, Domain: "https://n8n.example.com"},
		{Service: "n8n", Port: 5678, Domain: "http://n8n.test.sslip.io"},
	}
	variables := routeVariables(routes)
	// The first domain is the address of the service, under both names.
	for name, want := range map[string]string{
		"CAIS_HOST_N8N": "n8n.example.com", "CAIS_URL_N8N": "https://n8n.example.com",
		"PIER_HOST_N8N": "n8n.example.com", "PIER_URL_N8N": "https://n8n.example.com",
	} {
		if variables[name] != want {
			t.Errorf("%s = %q, want %q", name, variables[name], want)
		}
	}
	generated := generatedDefaults(raw, nil)
	if len(generated["CAIS_SECRET_ENCRYPTION"]) != 64 || len(generated["CAIS_PASSWORD_DB"]) != 32 {
		t.Errorf("legacy generated variables must be created like the current ones: %v", generated)
	}
	for _, variable := range doc.referencedVariables() {
		if variable.Name == "CAIS_HOST_N8N" && variable.Kind != VariableAddress {
			t.Errorf("a legacy address variable is an address: %+v", variable)
		}
	}
}
