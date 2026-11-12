package stack

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"pier/internal/platform/executor"
	"pier/internal/server"
)

func TestHostFilesRefuseSymlinksAndPreserveExistingFiles(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ex := executor.NewLocal()
	ctx := context.Background()
	file := hostFile{Path: "config/start.sh", Content: []byte("secret\n$verbatim"), Mode: 0o750}
	if err := createHostFile(ctx, ex, root, file); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, file.Path)
	content, _ := os.ReadFile(target)
	info, _ := os.Stat(target)
	if string(content) != string(file.Content) || info.Mode().Perm() != 0o750 {
		t.Fatalf("content/mode: %q %v", content, info.Mode())
	}
	os.WriteFile(target, []byte("operator"), 0o600)
	os.Chmod(target, 0o600)
	if err := createHostFile(ctx, ex, root, file); err != nil {
		t.Fatal(err)
	}
	content, _ = os.ReadFile(target)
	info, _ = os.Stat(target)
	if string(content) != "operator" || info.Mode().Perm() != 0o600 {
		t.Fatal("existing file changed")
	}
	for _, dangling := range []bool{false, true} {
		os.Remove(target)
		destination := filepath.Join(root, "outside")
		if !dangling {
			os.WriteFile(destination, []byte("untouched"), 0o400)
		} else {
			os.Remove(destination)
		}
		if err := os.Symlink(destination, target); err != nil {
			t.Fatal(err)
		}
		if err := createHostFile(ctx, ex, root, file); err == nil {
			t.Fatal("accepted symlink")
		}
		if !dangling {
			data, _ := os.ReadFile(destination)
			if string(data) != "untouched" {
				t.Fatal("symlink target changed")
			}
		}
	}
	outside := t.TempDir()
	os.Symlink(outside, filepath.Join(root, "linked"))
	file.Path = "linked/new.txt"
	if err := createHostFile(ctx, ex, root, file); err == nil {
		t.Fatal("accepted ancestor symlink")
	}
	if _, err := os.Stat(filepath.Join(outside, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("wrote outside stack")
	}
}

func TestDeleteConflictsWithRunningOperationEvenWhenForced(t *testing.T) {
	svc, fake := newService(t)
	ctx := context.Background()
	item, err := svc.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "test", Compose: "services:\n  web:\n    image: nginx:1.27\n"})
	if err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	if err := svc.runInBackground(ctx, item.ID, "Deploy da stack", func(context.Context, *stackRun) error { close(entered); <-release; return nil }); err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := svc.Delete(ctx, item.ID, true, true); err == nil {
		t.Fatal("delete accepted running operation")
	}
	if fake.Ran("rm -rf") {
		t.Fatal("cleanup ran during operation")
	}
	if _, err := svc.Get(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitOperation(t, svc, item.ID)
	if err := svc.Delete(ctx, item.ID, true, true); err != nil {
		t.Fatal(err)
	}
	if err := svc.Deploy(ctx, item.ID); err == nil {
		t.Fatal("deleted resource deployed")
	}
}

func TestRoutedServicesGetPersistedProxyAuthorizationAndLogging(t *testing.T) {
	svc, _ := newService(t)
	item, err := svc.Create(context.Background(), CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "test", Compose: "services:\n  web:\n    image: nginx:1.27\n  db:\n    image: postgres:17\n", Routes: []Route{{Service: "web", Port: 80, Domain: "https://example.com"}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := svc.reach(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Connector fixtures supply the same persisted server on every reach.
	if !reflect.DeepEqual(run.stack.proxyLabels, server.ProxyRouteLabels(run.target)) {
		t.Fatal("lost persisted route labels")
	}

	raw, _, err := render(run.stack)
	if err != nil {
		t.Fatal(err)
	}
	doc, _ := parseCompose(string(raw))
	services := doc["services"].(map[string]any)
	for name, definition := range services {
		service := definition.(map[string]any)
		labels := service["labels"].(map[string]any)
		if name == "web" && labels[server.ProxyLabel] != "persisted-test-token" {
			t.Fatal("routed service lacks token")
		}
		if name == "db" && labels[server.ProxyLabel] != nil {
			t.Fatal("private service authorized")
		}
		if service["logging"] == nil {
			t.Fatal("missing logging")
		}
	}
	override, err := renderOverride(run.stack, doc, []string{"web", "db"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(override), "persisted-test-token") != 1 {
		t.Fatal("override route authorization")
	}
}

func TestExtendedRisksAndSocketSource(t *testing.T) {
	doc, err := parseCompose(`services:
  app:
    image: example:1
    volumes:
    - onedev-docker-sock:/var/run/docker.sock
    - /etc/secrets:/secrets:ro
    - /etc/localtime:/etc/localtime:ro
    ports: ['127.0.0.1:123:123']
    cap_add: [SYS_ADMIN]
    security_opt: ['apparmor=unconfined']
`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"host-ports", "capabilities", "unconfined", "host-path"}
	if !reflect.DeepEqual(doc.risks(), want) {
		t.Fatalf("%v", doc.risks())
	}
}

func TestGeneratedExactLengths(t *testing.T) {
	values := generatedDefaults("${PIER_PASSWORD_16_REALTIME} ${PIER_HEX_64_CREDS} ${PIER_HEX_32_IV} ${PIER_BASE64_128_SESSION}", nil)
	if len(values["PIER_PASSWORD_16_REALTIME"]) != 16 || len(values["PIER_HEX_64_CREDS"]) != 64 || len(values["PIER_HEX_32_IV"]) != 32 || len(values["PIER_BASE64_128_SESSION"]) < 128 {
		t.Fatal(values)
	}
}

func TestBcryptDefaultsKeepTheMatchingPasswordAndNeverRegenerate(t *testing.T) {
	values := generatedDefaults("${PIER_BCRYPT_N8NOWNER}", nil)
	password, hash := values["PIER_PASSWORD_N8NOWNER"], values["PIER_BCRYPT_N8NOWNER"]
	if len(password) != 32 {
		t.Fatal("missing matching password")
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Fatal(err)
	}
	if again := generatedDefaults("${PIER_BCRYPT_N8NOWNER}", values); len(again) != 0 {
		t.Fatal("regenerated owner credentials")
	}
	existing := map[string]string{"PIER_PASSWORD_N8NOWNER": "existing-password"}
	values = generatedDefaults("${PIER_BCRYPT_N8NOWNER}", existing)
	if err := bcrypt.CompareHashAndPassword([]byte(values["PIER_BCRYPT_N8NOWNER"]), []byte(existing["PIER_PASSWORD_N8NOWNER"])); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogSecurityRegressionWiring(t *testing.T) {
	required := map[string][]string{
		"dozzle-with-auth":            {"dozzle_data:/data", "DOZZLE_AUTH_PROVIDER=simple"},
		"dozzle":                      {"dozzle_data:/data", "DOZZLE_AUTH_PROVIDER=simple"},
		"supabase":                    {"${PIER_PASSWORD_16_REALTIMEENC}", "${PIER_PASSWORD_64_REALTIME}"},
		"hatchet":                     {"hatchet-config:/hatchet/config", "hatchet-certs:/hatchet/certs", "SERVER_AUTH_COOKIE_INSECURE=false", "ADMIN_PASSWORD=Hx9k${PIER_PASSWORD_32_HATCHETADMIN}"},
		"supertokens-with-mysql":      {"API_KEYS=${PIER_PASSWORD_SUPERTOKENS}"},
		"supertokens-with-postgresql": {"API_KEYS=${PIER_PASSWORD_SUPERTOKENS}"},
		"portainer":                   {"portainer/portainer-ce:2.33.7-alpine", "--admin-password-file", "portainer_credentials:/credentials:ro"},
		"librechat":                   {"CREDS_KEY=${PIER_HEX_64_CREDS}", "CREDS_IV=${PIER_HEX_32_CREDS}"},
		"grist":                       {"${PIER_BASE64_128_SESSION}"},
		"n8n":                         {"N8N_INSTANCE_OWNER_PASSWORD_HASH=${PIER_BCRYPT_N8NOWNER}", "${OWNER_EMAIL:?}"},
		"odoo":                        {"odoo-config:/etc/odoo:ro", "${PIER_PASSWORD_ODOOADMIN}"},
		"sftpgo":                      {"${PIER_PASSWORD_SFTPGO}", "SFTPGO_DATA_PROVIDER__CREATE_DEFAULT_ADMIN"},
		"authentik":                   {"${PIER_PASSWORD_AUTHENTIKADMIN}", "${PIER_SECRET_AUTHENTIKTOKEN}"},
		"firefox":                     {"WEB_AUTHENTICATION", "${PIER_PASSWORD_FIREFOX}"},
		"libreoffice":                 {"${PIER_PASSWORD_LIBREOFFICE}"},
	}
	for slug, values := range required {
		template, ok := FindTemplate(slug)
		if !ok {
			t.Fatal(slug)
		}
		for _, value := range values {
			if !strings.Contains(template.Compose, value) {
				t.Errorf("%s missing %s", slug, value)
			}
		}
	}
	for _, slug := range []string{"honcho", "glances", "chroma", "mage-ai", "syncthing", "mindsdb", "kuzzle", "vault", "windmill"} {
		template, _ := FindTemplate(slug)
		if len(template.Routes) != 0 {
			t.Errorf("%s still suggests a public route: %v", slug, template.Routes)
		}
	}
	for _, slug := range []string{"flowise", "flowise-with-databases"} {
		template, _ := FindTemplate(slug)
		if template.Auth != "first-visitor" || strings.Contains(template.Compose, "FLOWISE_PASSWORD") {
			t.Errorf("%s misleading auth", slug)
		}
	}
	for _, slug := range []string{"penpot", "penpot-with-s3"} {
		template, _ := FindTemplate(slug)
		for _, route := range template.Routes {
			if route.Service == "mailpit" {
				t.Errorf("%s exposes recovery mail", slug)
			}
		}
	}
	for _, slug := range []string{"wings", "pterodactyl-with-wings"} {
		template, _ := FindTemplate(slug)
		for _, name := range []string{"WINGS_CONFIG"} {
			found := false
			for _, v := range template.Variables {
				if v.Name == name && v.Required {
					found = true
				}
			}
			if !found {
				t.Errorf("%s does not require %s", slug, name)
			}
		}
	}
}
