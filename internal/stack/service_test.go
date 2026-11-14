package stack

import (
	"context"
	"path/filepath"
	"pier/internal/docker"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/oplog"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1", ProxyToken: "persisted-test-token", Kind: server.KindLocal, DataDir: "/data/pier"}, nil
}

func newService(t *testing.T) (*Service, *executor.Fake) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range []string{
		`INSERT INTO servers (id, name, kind, data_dir, created_at) VALUES ('srv1', 'test', 'local', '/data/pier', '2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id, name, created_at) VALUES ('prj1', 'p', '2026-01-01T00:00:00Z')`,
		`INSERT INTO environments (id, project_id, name, created_at) VALUES ('env1', 'prj1', 'production', '2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	box := testbox.Box(t)
	fake := executor.NewFake()
	return NewService(db, envvar.NewStore(db, box), fakeConnector{fake}, oplog.NewRegistry()), fake
}

// waitOperation blocks until the background operation of a stack ends.
func waitOperation(t *testing.T, service *Service, id string) oplog.Snapshot {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot := service.Operation(id); !snapshot.Running && snapshot.FinishedAt != nil {
			return snapshot
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the operation did not finish")
	return oplog.Snapshot{}
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected a validation error, got %v", err)
	}
	return appErr.Fields
}

const folder = "/home/deploy/shop"

const folderCompose = `
services:
  web:
    image: nginx:alpine
    env_file: app.env
    secrets: [db_password]
    volumes:
      - ./html:/usr/share/nginx/html:ro
  worker:
    build: ./worker
    restart: always
    networks: [internal]
networks:
  internal:
secrets:
  db_password:
    file: ./secrets/db_password.txt
`

func directoryInput() CreateInput {
	return CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "shop", Directory: folder + "/",
		Routes: []Route{{Service: "web", Port: 80, Domain: "https://Shop.Example.com"}},
	}
}

func TestDirectoryStackFindsTheComposeFileAndKeepsNoCopyOfIt(t *testing.T) {
	service, fake := newService(t)
	// Only docker-compose.yml exists: the standard names are tried in order.
	for _, missing := range []string{"compose.yaml", "compose.yml", "docker-compose.yaml"} {
		fake.On("test -f "+folder+"/"+missing, "", 1)
	}
	fake.Files[folder+"/docker-compose.yml"] = []byte(folderCompose)

	stack, err := service.Create(context.Background(), directoryInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if stack.Directory != folder || stack.ComposeFile != "docker-compose.yml" || stack.Compose != "" {
		t.Errorf("the folder is the source and nothing is copied: %+v", stack)
	}
	if stack.Routes[0].Domain != "https://shop.example.com" {
		t.Errorf("routes are validated against the file of the folder: %+v", stack.Routes)
	}
	stored, _ := service.Get(context.Background(), stack.ID)
	if stored.Directory != folder || stored.ComposeFile != "docker-compose.yml" || !stored.FromDirectory() {
		t.Errorf("stored stack: %+v", stored)
	}
	services, err := service.Services(context.Background(), stack.ID)
	if err != nil || strings.Join(services, ",") != "web,worker" {
		t.Errorf("services are read from the folder: %v, %v", services, err)
	}
}

func TestDirectoryStackRejectsWhatCannotWork(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		prepare func(fake *executor.Fake, input *CreateInput)
		field   string
	}{
		{"relative path", func(_ *executor.Fake, input *CreateInput) { input.Directory = "projects/shop" }, "directory"},
		{"the root", func(_ *executor.Fake, input *CreateInput) { input.Directory = "/" }, "directory"},
		{"pier own data", func(_ *executor.Fake, input *CreateInput) { input.Directory = "/data/pier/stacks/abc" }, "directory"},
		{"missing folder", func(fake *executor.Fake, _ *CreateInput) { fake.On("test -d "+folder, "", 1) }, "directory"},
		{"no compose file", func(fake *executor.Fake, _ *CreateInput) { fake.On("test -f", "", 1) }, "directory"},
		{"file outside the folder", func(_ *executor.Fake, input *CreateInput) { input.ComposeFile = "../other/compose.yaml" }, "compose_file"},
		{"not a compose file", func(_ *executor.Fake, input *CreateInput) { input.ComposeFile = "notes.txt" }, "compose_file"},
		{"unreadable file", func(_ *executor.Fake, input *CreateInput) { input.ComposeFile = "missing.yaml" }, "compose_file"},
		{"two sources", func(_ *executor.Fake, input *CreateInput) { input.Template = "whoami" }, "directory"},
		{"bad project name", func(_ *executor.Fake, input *CreateInput) { input.ProjectName = "My Shop" }, "project_name"},
		{"route to a missing service", func(_ *executor.Fake, input *CreateInput) { input.Routes[0].Service = "api" }, "routes.0"},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			service, fake := newService(t)
			fake.Files[folder+"/compose.yaml"] = []byte(folderCompose)
			input := directoryInput()
			item.prepare(fake, &input)
			_, err := service.Create(ctx, input)
			if fields := fieldsOf(t, err); fields[item.field] == "" {
				t.Errorf("expected an error on %q, got %v", item.field, fields)
			}
			if stacks, _ := service.List(ctx, ""); len(stacks) != 0 {
				t.Errorf("nothing must be stored: %+v", stacks)
			}
		})
	}
}

func TestProjectNamesAreUniqueAndOnlyForDirectoryStacks(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	fake.Files[folder+"/compose.yaml"] = []byte(folderCompose)
	input := directoryInput()
	input.ProjectName = "shop"
	if _, err := service.Create(ctx, input); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if fields := fieldsOf(t, func() error { _, err := service.Create(ctx, input); return err }()); fields["project_name"] == "" {
		t.Errorf("two stacks sharing a project would overwrite each other: %v", fields)
	}
	inline := CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "x", Compose: "services:\n  a:\n    image: x\n", ProjectName: "other"}
	if fields := fieldsOf(t, func() error { _, err := service.Create(ctx, inline); return err }()); fields["directory"] == "" {
		t.Errorf("a project name makes no sense outside a directory stack: %v", fields)
	}
}

// The compose file, env files and secrets of the folder are used where they
// are; Pier only adds an override file in its own directory.
func TestDeployingADirectoryStackLeavesTheFolderUntouched(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	fake.Files[folder+"/compose.yaml"] = []byte(folderCompose)
	fake.On("config --services", "web\nworker\nmetrics", 0)
	fake.On("config --format json", `{"services":{"web":{"image":"nginx:alpine"}}}`, 0)
	fake.On("docker image inspect", "null", 0)
	input := directoryInput()
	input.Env = map[string]string{"LOG_LEVEL": "debug"}
	stack, err := service.Create(ctx, input)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	filesBefore := len(fake.Files)
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if snapshot := waitOperation(t, service, stack.ID); snapshot.Error != "" {
		t.Fatalf("deploy failed: %s\n%s", snapshot.Error, strings.Join(snapshot.Lines, "\n"))
	}

	own := "/data/pier/stacks/" + stack.ID
	up := ""
	for _, command := range fake.Commands {
		if strings.Contains(command, " up ") {
			up = command
		}
	}
	for _, fragment := range []string{
		"--project-name " + docker.ReadableName(stack.Name, stack.ID), "--project-directory " + folder,
		"--file " + folder + "/compose.yaml --file " + own + "/docker-compose.yaml",
		// The .env of the folder first, then the one of Pier, which wins.
		"--env-file " + folder + "/.env --env-file " + own + "/.env",
		"up --detach --remove-orphans --build",
	} {
		if !strings.Contains(up, fragment) {
			t.Errorf("the up command lacks %q:\n%s", fragment, up)
		}
	}

	if string(fake.Files[folder+"/compose.yaml"]) != folderCompose {
		t.Error("the compose file of the folder was modified")
	}
	if len(fake.Files) != filesBefore+2 {
		t.Errorf("only the override and the env file of Pier may be written: %v", keys(fake.Files))
	}
	for written := range fake.Files {
		if strings.HasPrefix(written, folder) && written != folder+"/compose.yaml" {
			t.Errorf("Pier wrote inside the folder of the user: %s", written)
		}
	}

	var override struct {
		Services map[string]struct {
			Labels   map[string]string `yaml:"labels"`
			Networks map[string]any    `yaml:"networks"`
			Restart  string            `yaml:"restart"`
			Image    string            `yaml:"image"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(fake.Files[own+"/docker-compose.yaml"], &override); err != nil {
		t.Fatalf("override is not valid YAML: %v", err)
	}
	web, worker, metrics := override.Services["web"], override.Services["worker"], override.Services["metrics"]
	if web.Labels["pier.id"] != stack.ID || web.Labels["traefik.enable"] != "true" || web.Restart != "unless-stopped" {
		t.Errorf("web override: %+v", web)
	}
	if _, joined := web.Networks[ingressKey]; !joined || len(web.Networks) != 2 {
		t.Errorf("the routed service joins the ingress network and keeps the default one: %v", web.Networks)
	}
	// "worker" declares its own restart policy and networks: both stay as they are.
	if worker.Restart != "" || worker.Networks != nil || worker.Labels["pier.managed"] != "true" {
		t.Errorf("worker override: %+v", worker)
	}
	// "metrics" comes from an include: Pier cannot see its definition, so it
	// only labels it.
	if metrics.Labels["pier.id"] != stack.ID || metrics.Restart != "" {
		t.Errorf("metrics override: %+v", metrics)
	}
	if web.Image != "" {
		t.Error("the override must not redefine what the folder defines")
	}
	if dotenv := string(fake.Files[own+"/.env"]); !strings.Contains(dotenv, "LOG_LEVEL") || !strings.Contains(dotenv, "PIER_URL_WEB") {
		t.Errorf("the env file of Pier carries its variables and the published address:\n%s", dotenv)
	}
}

func keys(files map[string][]byte) []string {
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	return names
}

func TestRemovingADirectoryStackNeverRemovesTheFolder(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	fake.Files[folder+"/compose.yaml"] = []byte(folderCompose)
	stack, err := service.Create(ctx, directoryInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := service.Delete(ctx, stack.ID, true, false); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	for _, command := range fake.Commands {
		if strings.HasPrefix(command, "rm ") && !strings.Contains(command, "/data/pier/stacks/"+stack.ID) {
			t.Errorf("only the directory of Pier may be removed: %s", command)
		}
		if strings.HasPrefix(command, "rm ") && strings.Contains(command, folder) {
			t.Errorf("the folder of the user was touched: %s", command)
		}
	}
	if !fake.Ran("rm -rf /data/pier/stacks/" + stack.ID) {
		t.Errorf("the override directory should be gone; ran:\n%s", strings.Join(fake.Commands, "\n"))
	}
}

// A catalog template may ship files (an entrypoint, a config) inside the
// compose file; they must exist on the server before "up".
func TestDeployWritesTheFilesATemplateCarriesButKeepsEditedOnes(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "registry", Template: "docker-registry"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	own := "/data/pier/stacks/" + stack.ID
	// Nothing exists yet on the first deployment.
	fake.On("test -e", "", 0)
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if snapshot := waitOperation(t, service, stack.ID); snapshot.Error != "" {
		t.Fatalf("deploy failed: %s", snapshot.Error)
	}
	script := ""
	for command, input := range fake.Inputs {
		if strings.Contains(command, "entrypoint.sh") {
			script = string(input)
		}
	}
	if !strings.HasPrefix(script, "#!/usr/bin/env sh") || !strings.Contains(script, `"$USERNAME"`) {
		t.Errorf("the entrypoint must be written verbatim:\n%s", script)
	}
	rendered := string(fake.Files[own+"/docker-compose.yaml"])
	if strings.Contains(rendered, "content:") || strings.Contains(rendered, "isDirectory") {
		t.Errorf("extensions must not reach docker compose:\n%s", rendered)
	}
	dotenv := string(fake.Files[own+"/.env"])
	if !strings.Contains(dotenv, "PIER_PASSWORD_REGISTRY=") || !strings.Contains(dotenv, "PIER_USER_REGISTRY=") {
		t.Errorf("generated credentials must be in the env file:\n%s", dotenv)
	}

	// On the next deployment the file exists and was edited: it is kept.
	fake.On("test -e", "", 0)
	fake.Files[own+"/etc/entrypoint.sh"] = []byte("edited by the operator")
	if err := service.Deploy(ctx, stack.ID); err != nil {
		t.Fatalf("second Deploy: %v", err)
	}
	waitOperation(t, service, stack.ID)
	if string(fake.Files[own+"/etc/entrypoint.sh"]) != "edited by the operator" {
		t.Error("a file changed on the server must not be overwritten")
	}
	// And the credentials did not change between deployments.
	if again := string(fake.Files[own+"/.env"]); again != dotenv {
		t.Errorf("generated values changed between deployments:\n%s\n---\n%s", dotenv, again)
	}
}

func TestVariablesTellWhatIsStillMissing(t *testing.T) {
	ctx := context.Background()
	service, _ := newService(t)
	stack, err := service.Create(ctx, CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "app",
		Compose: `
services:
  app:
    image: "example/app:${TAG:-1}"
    environment:
      - 'KEY=${APP_KEY:?}'
      - 'URL=${PIER_URL_APP}'
      - 'PASS=${PIER_PASSWORD_APP}'
`,
		Env: map[string]string{"TAG": "2"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	variables, err := service.Variables(ctx, stack.ID)
	if err != nil {
		t.Fatalf("Variables: %v", err)
	}
	status := map[string]VariableStatus{}
	for _, variable := range variables {
		status[variable.Name] = variable
	}
	if !status["TAG"].Set || !status["PIER_PASSWORD_APP"].Set {
		t.Errorf("stored and generated variables are set: %+v", status)
	}
	if missing := status["APP_KEY"]; missing.Set || !missing.Required {
		t.Errorf("APP_KEY is required and missing: %+v", missing)
	}
	if status["PIER_URL_APP"].Set || status["PIER_URL_APP"].Kind != VariableAddress {
		t.Errorf("without a domain the address is not set: %+v", status["PIER_URL_APP"])
	}
}

// The .env of the folder is where a directory stack keeps its secrets. Pier
// reports which names it defines so nothing looks missing, without ever
// returning a value.
func TestVariablesOfADirectoryStackKnowTheNamesInItsDotenv(t *testing.T) {
	ctx := context.Background()
	service, fake := newService(t)
	fake.Files[folder+"/compose.yaml"] = []byte("services:\n  web:\n    image: \"app:${TAG}\"\n    environment:\n" +
		"      - 'TOKEN=${API_TOKEN}'\n      - 'MODE=${MODE}'\n      - 'OTHER=${NOT_DEFINED}'\n")
	fake.Files[folder+"/.env"] = []byte("# comment\nAPI_TOKEN=super-secret-value\nexport TAG = 1.2\n  MODE=\"prod\"\n")
	stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "shop", Directory: folder,
		Env: map[string]string{"MODE": "staging"}})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	variables, err := service.Variables(ctx, stack.ID)
	if err != nil {
		t.Fatalf("Variables: %v", err)
	}
	sources := map[string]string{}
	for _, variable := range variables {
		sources[variable.Name] = variable.Source
		if variable.Set != (variable.Source != "") {
			t.Errorf("%s: set and source disagree: %+v", variable.Name, variable)
		}
		if strings.Contains(variable.Default, "secret") {
			t.Errorf("a value of the .env leaked: %+v", variable)
		}
	}
	// A value kept by Pier wins over the folder, as in the deployment.
	want := map[string]string{"API_TOKEN": ValueFolder, "TAG": ValueFolder, "MODE": ValueStored, "NOT_DEFINED": ""}
	for name, source := range want {
		if sources[name] != source {
			t.Errorf("%s comes from %q, want %q", name, sources[name], source)
		}
	}
}

// A stack with a domain on a server whose proxy is stopped deploys, but its
// links do not answer. That is how a real n8n ended up unreachable: the
// deployment must say so.
func TestDeployWarnsWhenTheProxyIsStopped(t *testing.T) {
	ctx := context.Background()
	for _, running := range []bool{false, true} {
		service, fake := newService(t)
		fake.On("config --format json", `{"services":{"whoami":{"image":"traefik/whoami"}}}`, 0)
		fake.On("docker image inspect", "null", 0)
		if running {
			fake.On("docker inspect --format", "true", 0)
		} else {
			fake.On("docker inspect --format", "Error: No such object: pier-proxy", 1)
		}
		stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "web", Template: "whoami",
			Routes: []Route{{Service: "whoami", Port: 80, Domain: "https://whoami.example.com"}}})
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := service.Deploy(ctx, stack.ID); err != nil {
			t.Fatalf("Deploy: %v", err)
		}
		snapshot := waitOperation(t, service, stack.ID)
		warned := strings.Contains(strings.Join(snapshot.Lines, "\n"), "proxy deste servidor está parado")
		if warned == running {
			t.Errorf("proxy running=%v: warned=%v\n%s", running, warned, strings.Join(snapshot.Lines, "\n"))
		}
	}
}

// Volumes and networks are named after the compose project, so the name a
// stack runs under is decided once and never follows a rename; stacks from
// before readable names keep running under their id.
func TestTheComposeProjectOfAStackNeverChanges(t *testing.T) {
	created := Stack{ID: "k3j2xq8wz1ab4c", Name: "n8n", ComposeProject: "n8n-k3j2xq"}
	renamed := created
	renamed.Name = "Automação"
	if created.composeProject() != "n8n-k3j2xq" || renamed.composeProject() != "n8n-k3j2xq" {
		t.Errorf("a rename must not move the stack to another project: %q", renamed.composeProject())
	}
	legacy := Stack{ID: "k3j2xq8wz1ab4c", Name: "n8n"}
	if legacy.composeProject() != legacy.ID {
		t.Errorf("a stack created before readable names keeps its id, got %q", legacy.composeProject())
	}
	chosen := Stack{ID: "k3j2xq8wz1ab4c", ProjectName: "meu-projeto", ComposeProject: "ignored"}
	if chosen.composeProject() != "meu-projeto" {
		t.Errorf("the project name chosen for a directory stack wins, got %q", chosen.composeProject())
	}
}
