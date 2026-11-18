package stack

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"testing"

	"pier/internal/application"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
)

const (
	gitToken     = "ghp_secrettokenvalue123"
	gitRepo      = "https://octo:" + gitToken + "@github.com/acme/shop.git"
	gitCommit    = "0123456789abcdef0123456789abcdef01234567"
	gitCompose   = "services:\n  web:\n    image: nginx:alpine\n    env_file: ./app.env\n    volumes:\n      - ./html:/usr/share/nginx/html:ro\n"
	gitBuildFile = "services:\n  web:\n    build:\n      context: ./web\n      dockerfile: Dockerfile.prod\n"
)

type fakeKeys struct{ missing bool }

func (k fakeKeys) Material(_ context.Context, id string) (string, error) {
	if k.missing {
		return "", errors.New("not found")
	}
	return "PRIVATE-KEY-" + id, nil
}

func newGitService(t *testing.T) (*Service, *executor.Fake) {
	t.Helper()
	service, fake := newService(t)
	box := testbox.Box(t)
	service.SetGitSupport(box, fakeKeys{})
	return service, fake
}

func gitInput() CreateInput {
	return CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "shop",
		Git:    &GitInput{Repository: gitRepo, Branch: "main"},
		Routes: []Route{{Service: "web", Port: 80, Domain: "https://Shop.Example.com"}},
	}
}

func checkoutOf(stack Stack) string { return "/data/pier/stacks/" + stack.ID + "/repo" }

func scratchOf(stack Stack) string { return "/data/pier/builds/stack-" + stack.ID + "/src" }

// seedClone makes the fake behave like a successful clone of compose.
func seedClone(fake *executor.Fake, stack Stack, composeFile, content string) {
	fake.On("alpine/git", "PIER_COMMIT "+gitCommit+" add shop", 0)
	fake.Files[scratchOf(stack)+"/"+composeFile] = []byte(content)
	fake.Files[checkoutOf(stack)+"/"+composeFile] = []byte(content)
	fake.On("config --services", "web", 0)
	fake.On("config --format json", `{"services":{"web":{"image":"nginx:alpine"}}}`, 0)
	fake.On("docker image inspect", "null", 0)
	fake.On("docker info", "20971520", 0)
}

func deployAndWait(t *testing.T, service *Service, id string) (string, error) {
	t.Helper()
	if err := service.Deploy(context.Background(), id); err != nil {
		return "", err
	}
	snapshot := waitOperation(t, service, id)
	if snapshot.Error != "" {
		return strings.Join(snapshot.Lines, "\n"), errors.New(snapshot.Error)
	}
	return strings.Join(snapshot.Lines, "\n"), nil
}

func TestGitStackIsRegisteredWithItsCheckoutAndSealedRepository(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	stack, err := service.Create(ctx, gitInput())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !stack.FromGit() || stack.Directory != checkoutOf(stack) || stack.ComposeFile != DefaultComposePath {
		t.Errorf("stack: %+v", stack)
	}
	if stack.Git.Branch != "main" || stack.Git.ComposePath != DefaultComposePath || !stack.Git.AutoDeploy {
		t.Errorf("defaults: %+v", stack.Git)
	}
	if stack.Routes[0].Domain != "https://shop.example.com" {
		t.Errorf("routes are canonicalized before the first deployment: %+v", stack.Routes)
	}
	var raw string
	if err := service.db.QueryRow(`SELECT repository FROM stack_git_sources WHERE stack_id = ?`, stack.ID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, gitToken) {
		t.Error("the repository, which carries a token, must be stored sealed")
	}
	stored, _ := service.Get(ctx, stack.ID)
	if stored.Git.Repository != gitRepo {
		t.Errorf("stored repository: %q", stored.Git.Repository)
	}
	if hidden := stored.WithoutSecrets(); strings.Contains(hidden.Git.Repository, gitToken) || stored.Git.Repository != gitRepo {
		t.Errorf("WithoutSecrets must hide the token without touching the original: %q", hidden.Git.Repository)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("creating must not touch the server: %v", fake.Commands)
	}
	if secret, err := service.WebhookSecret(ctx, stack.ID); err != nil || len(secret) < 32 {
		t.Errorf("webhook secret: %q %v", secret, err)
	}
	if services, err := service.Services(ctx, stack.ID); err != nil || len(services) != 0 {
		t.Errorf("before the first deployment there is nothing to list: %v %v", services, err)
	}
	listed, _ := service.List(ctx, "env1")
	if len(listed) != 1 || listed[0].Git == nil {
		t.Errorf("List must carry the Git source: %+v", listed)
	}
}

func TestGitStackRejectsWhatCannotWork(t *testing.T) {
	cases := []struct {
		name   string
		change func(*CreateInput)
		field  string
	}{
		{"local transport", func(i *CreateInput) { i.Git.Repository = "file:///etc" }, "git.repository"},
		{"ssh without key", func(i *CreateInput) { i.Git.Repository = "git@github.com:acme/shop.git" }, "git.private_key_id"},
		{"escaping base directory", func(i *CreateInput) { i.Git.BaseDirectory = "../x" }, "git.base_directory"},
		{"compose path not yaml", func(i *CreateInput) { i.Git.ComposePath = "run.sh" }, "git.compose_path"},
		{"compose path escapes", func(i *CreateInput) { i.Git.ComposePath = "../x.yaml" }, "git.compose_path"},
		{"bad branch", func(i *CreateInput) { i.Git.Branch = "a..b" }, "git.branch"},
		{"bad watch path", func(i *CreateInput) { i.Git.WatchPaths = []string{"/abs"} }, "git.watch_paths"},
		{"unknown risk", func(i *CreateInput) { i.Git.AcceptedRisks = []string{"nope"} }, "git.accepted_risks"},
		{"two sources", func(i *CreateInput) { i.Compose = gitCompose }, "git.repository"},
		{"bad route port", func(i *CreateInput) { i.Routes[0].Port = 0 }, "routes.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			service, _ := newGitService(t)
			input := gitInput()
			c.change(&input)
			_, err := service.Create(context.Background(), input)
			if _, rejected := fieldsOf(t, err)[c.field]; !rejected {
				t.Errorf("expected a problem on %s, got %v", c.field, err)
			}
		})
	}
	t.Run("unknown deploy key", func(t *testing.T) {
		service, _ := newGitService(t)
		service.keys = fakeKeys{missing: true}
		input := gitInput()
		input.Git.PrivateKeyID = "gone"
		if _, err := service.Create(context.Background(), input); err == nil {
			t.Fatal("a deploy key that does not exist must be rejected")
		}
	})
}

func TestGitDeployClonesInspectsInstallsAndRunsThePipeline(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	stack, _ := service.Create(ctx, gitInput())
	seedClone(fake, stack, DefaultComposePath, gitCompose)

	if log, err := deployAndWait(t, service, stack.ID); err != nil {
		t.Fatalf("deploy: %v\n%s", err, log)
	}
	var clone, install, up string
	for _, command := range fake.Commands {
		switch {
		case strings.Contains(command, "alpine/git") && strings.Contains(command, "git clone"):
			clone = command
		case strings.Contains(command, `mv "$2"`):
			install = command
		case strings.Contains(command, " up "):
			up = command
		}
	}
	if clone == "" || strings.Contains(clone, gitToken) {
		t.Errorf("the clone runs in a container and the token never reaches a command line:\n%s", clone)
	}
	envFile := string(fake.Files["/data/pier/builds/stack-"+stack.ID+"/clone.env"])
	if !strings.Contains(envFile, "PIER_BRANCH=main") || !strings.Contains(envFile, "Authorization: Basic") ||
		strings.Contains(envFile, gitToken) {
		t.Errorf("clone environment (token only as header): %s", envFile)
	}
	if !strings.Contains(install, checkoutOf(stack)) && !strings.Contains(install, "/data/pier/stacks/"+stack.ID) {
		t.Errorf("the inspected checkout replaces the live one: %s", install)
	}
	for _, fragment := range []string{"--project-directory " + checkoutOf(stack), "--file " + checkoutOf(stack) + "/docker-compose.yaml"} {
		if !strings.Contains(up, fragment) {
			t.Errorf("up lacks %q:\n%s", fragment, up)
		}
	}
	stored, _ := service.Get(ctx, stack.ID)
	if stored.Git.LastCommit != gitCommit || stored.Git.LastCommitMessage != "add shop" || stored.Git.LastRef != "main" ||
		stored.Git.LastDeployedAt == nil {
		t.Errorf("last deployment not recorded: %+v", stored.Git)
	}
	services, _ := service.Services(ctx, stack.ID)
	if strings.Join(services, ",") != "web" {
		t.Errorf("services come from the checkout: %v", services)
	}
}

func TestGitDeployWithBuildSectionTakesTheBuildSlotAndChecksTheDisk(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	input := gitInput()
	input.Git.BaseDirectory = "deploy"
	input.Git.ComposePath = "compose.yaml"
	input.Routes = nil
	stack, err := service.Create(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	seedClone(fake, stack, "deploy/compose.yaml", gitBuildFile)
	if log, err := deployAndWait(t, service, stack.ID); err != nil {
		t.Fatalf("a build section is valid: %v\n%s", err, log)
	}
	if !fake.Ran("docker info") {
		t.Error("the free disk space is checked before building, as for applications")
	}
	if !fake.Ran("docker compose --project-name") || !fake.Ran(" build") {
		t.Errorf("images are built on the server: %v", fake.Commands)
	}
}

func TestGitDeployLowDiskRefusesTheBuild(t *testing.T) {
	service, fake := newGitService(t)
	stack, _ := service.Create(context.Background(), CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "b", Git: &GitInput{Repository: "https://github.com/a/b.git"}})
	seedClone(fake, stack, DefaultComposePath, gitBuildFile)
	fake.On("docker info", "1024", 0)
	_, err := deployAndWait(t, service, stack.ID)
	if err == nil || !strings.Contains(err.Error(), "espaço insuficiente") {
		t.Fatalf("expected the disk limit, got %v", err)
	}
	if fake.Ran("up --detach") {
		t.Error("nothing starts after a refused build")
	}
}

func TestGitDeployRefusesPathsThatEscapeTheCheckout(t *testing.T) {
	cases := map[string]string{
		"env file":        "services:\n  web:\n    image: x\n    env_file: ../../etc/passwd\n",
		"bind mount":      "services:\n  web:\n    image: x\n    volumes:\n      - ../secrets:/s\n",
		"long bind":       "services:\n  web:\n    image: x\n    volumes:\n      - type: bind\n        source: ../../root\n        target: /r\n",
		"build context":   "services:\n  web:\n    build: ../../\n",
		"dockerfile":      "services:\n  web:\n    build:\n      context: ./app\n      dockerfile: ../../Dockerfile\n",
		"include":         "include:\n  - ../other.yaml\nservices:\n  web:\n    image: x\n",
		"extends":         "services:\n  web:\n    extends:\n      file: ../../base.yaml\n      service: a\n",
		"secret file":     "services:\n  web:\n    image: x\nsecrets:\n  s:\n    file: ../../key\n",
		"unresolved var":  "services:\n  web:\n    image: x\n    volumes:\n      - ${DATA}/x:/x\n",
		"home":            "services:\n  web:\n    image: x\n    env_file: ~/.env\n",
		"path with entry": "services:\n  web:\n    image: x\n    env_file:\n      - path: ../../x.env\n",
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			service, fake := newGitService(t)
			stack, _ := service.Create(context.Background(), CreateInput{
				EnvironmentID: "env1", ServerID: "srv1", Name: "e", Git: &GitInput{Repository: "https://github.com/a/b.git"}})
			seedClone(fake, stack, DefaultComposePath, content)
			_, err := deployAndWait(t, service, stack.ID)
			if err == nil || !strings.Contains(err.Error(), "sai do repositório") {
				t.Fatalf("expected a rejection, got %v", err)
			}
			if fake.Ran(`mv "$2"`) || fake.Ran("up --detach") {
				t.Error("a rejected checkout must not replace the live one nor start anything")
			}
		})
	}
}

func TestGitDeployAcceptsPathsInsideTheCheckout(t *testing.T) {
	units := []composeUnit{{file: "deploy/compose.yaml", doc: map[string]any{
		"services": map[string]any{"web": map[string]any{
			"env_file": []any{"./a.env", "../shared.env"},
			"build":    map[string]any{"context": "../app", "dockerfile": "Dockerfile"},
			"volumes":  []any{"./data:/d", "named:/n", "${DATA:-./x}:/x", "/etc/localtime:/etc/localtime:ro", "named2:/m"},
		}},
	}}}
	if err := checkContained(units); err != nil {
		t.Fatalf("paths inside the repository are fine: %v", err)
	}
	remote := []composeUnit{{file: "compose.yaml", doc: map[string]any{"services": map[string]any{
		"web": map[string]any{"build": "https://github.com/a/b.git#main"}}}}}
	if err := checkContained(remote); err != nil {
		t.Fatalf("a remote build context is not a path: %v", err)
	}
}

func TestGitDeployRefusesSymlinksThatEscape(t *testing.T) {
	service, fake := newGitService(t)
	stack, _ := service.Create(context.Background(), CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "s", Git: &GitInput{Repository: "https://github.com/a/b.git"}})
	seedClone(fake, stack, DefaultComposePath, gitCompose)
	fake.On("readlink -f", "outside:/work/src/data", 0)
	_, err := deployAndWait(t, service, stack.ID)
	if err == nil || !strings.Contains(err.Error(), "link simbólico") {
		t.Fatalf("expected the symlink to be refused: %v", err)
	}
	if fake.Ran(`mv "$2"`) {
		t.Error("the live checkout must stay")
	}
}

func TestGitDeployRequiresAcceptingTheRisksOfTheRepository(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	stack, _ := service.Create(ctx, CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "r", Git: &GitInput{Repository: "https://github.com/a/b.git"}})
	risky := "services:\n  web:\n    image: x\n    privileged: true\n    volumes:\n      - /var/run/docker.sock:/var/run/docker.sock\n"
	seedClone(fake, stack, DefaultComposePath, risky)
	_, err := deployAndWait(t, service, stack.ID)
	if err == nil || !strings.Contains(err.Error(), "privileged") || !strings.Contains(err.Error(), "docker-socket") {
		t.Fatalf("risks must be named: %v", err)
	}
	stored, _ := service.Get(ctx, stack.ID)
	if strings.Join(stored.Git.PendingRisks, ",") != "docker-socket,privileged,host-path" {
		t.Errorf("pending risks are kept for the screen: %v", stored.Git.PendingRisks)
	}
	if fake.Ran("up --detach") {
		t.Fatal("nothing starts while risks are pending")
	}
	_, err = service.Update(ctx, stack.ID, UpdateInput{Name: "r", Git: &GitInput{AcceptedRisks: stored.Git.PendingRisks}})
	if err != nil {
		t.Fatalf("accept the risks: %v", err)
	}
	if log, err := deployAndWait(t, service, stack.ID); err != nil {
		t.Fatalf("accepted risks deploy: %v\n%s", err, log)
	}
	if again, _ := service.Get(ctx, stack.ID); len(again.Git.PendingRisks) != 0 {
		t.Errorf("pending risks clear after a deployment: %v", again.Git.PendingRisks)
	}
}

func TestGitPrivateKeyIsWrittenForTheCloneAndRemovedAfterwards(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "k",
		Git: &GitInput{Repository: "git@github.com:acme/shop.git", PrivateKeyID: "key1"}})
	if err != nil {
		t.Fatal(err)
	}
	seedClone(fake, stack, DefaultComposePath, gitCompose)
	if log, err := deployAndWait(t, service, stack.ID); err != nil {
		t.Fatalf("%v\n%s", err, log)
	}
	keyPath := "/data/pier/builds/stack-" + stack.ID + "/deploy_key"
	if string(fake.Files[keyPath]) != "PRIVATE-KEY-key1" {
		t.Error("the deploy key goes to a file the clone container mounts")
	}
	if !fake.Ran("rm -f") || !fake.Ran(keyPath+":/tmp/deploy_key:ro") ||
		!fake.Ran("/data/pier/stacks/"+stack.ID+"/git:/trust") {
		t.Errorf("key mounted read-only with host keys pinned per stack, then removed: %v", fake.Commands)
	}
}

func TestGitStackDeletionRemovesTheCheckout(t *testing.T) {
	ctx := context.Background()
	service, fake := newGitService(t)
	stack, _ := service.Create(ctx, gitInput())
	if err := service.Delete(ctx, stack.ID, false, false); err != nil {
		t.Fatal(err)
	}
	if !fake.Ran("-rf /parent/repo") {
		t.Errorf("the checkout is removed through a container: %v", fake.Commands)
	}
	var count int
	service.db.QueryRow(`SELECT COUNT(*) FROM stack_git_sources`).Scan(&count)
	if count != 0 {
		t.Error("the Git source goes with the stack")
	}
}

type freezeGuard struct{}

func (freezeGuard) CheckDeploy(context.Context, string) error {
	return apperr.Conflict("O ambiente está congelado.")
}

func signed(secret string, body []byte) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	header := http.Header{}
	header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	header.Set("X-GitHub-Event", "push")
	return header
}

func TestGitPushWebhook(t *testing.T) {
	ctx := context.Background()
	push := func(ref string, files ...string) []byte {
		added := `[]`
		if len(files) > 0 {
			added = `["` + strings.Join(files, `","`) + `"]`
		}
		return []byte(`{"ref":"` + ref + `","after":"` + gitCommit + `","commits":[{"added":[],"modified":` + added + `,"removed":[]}]}`)
	}
	setup := func(t *testing.T, git GitInput) (*Service, *executor.Fake, Stack, string) {
		service, fake := newGitService(t)
		git.Repository = "https://github.com/a/b.git"
		stack, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "w", Git: &git})
		if err != nil {
			t.Fatal(err)
		}
		seedClone(fake, stack, DefaultComposePath, gitCompose)
		secret, _ := service.WebhookSecret(ctx, stack.ID)
		return service, fake, stack, secret
	}

	t.Run("signed push on the branch deploys", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		body := push("refs/heads/main", "README.md")
		result, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, signed(secret, body), body)
		if err != nil || !result.Deployed {
			t.Fatalf("result %+v err %v", result, err)
		}
		if snapshot := waitOperation(t, service, stack.ID); snapshot.Error != "" {
			t.Errorf("redeploy failed: %s", snapshot.Error)
		}
	})
	t.Run("bad signature", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		body := push("refs/heads/main")
		header := signed("another-secret", body)
		if _, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, header, body); err == nil {
			t.Fatal("a wrong signature must be refused")
		}
		if _, err := service.HandleGitPush(ctx, "missing", application.ProviderGitHub, signed(secret, body), body); err == nil {
			t.Fatal("an unknown stack is refused like a bad signature")
		}
		if service.Operation(stack.ID).Running {
			t.Error("nothing deploys")
		}
	})
	t.Run("gitlab token and gitea signature", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		body := push("refs/heads/main")
		gitlab := http.Header{"X-Gitlab-Token": {secret}, "X-Gitlab-Event": {"Push Hook"}}
		if result, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitLab, gitlab, body); err != nil || !result.Deployed {
			t.Fatalf("gitlab: %+v %v", result, err)
		}
		waitOperation(t, service, stack.ID)
		gitea := signed(secret, body)
		gitea.Del("X-Hub-Signature-256")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		gitea.Set("X-Gitea-Signature", hex.EncodeToString(mac.Sum(nil)))
		gitea.Set("X-Gitea-Event", "push")
		if result, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitea, gitea, body); err != nil || !result.Deployed {
			t.Fatalf("gitea: %+v %v", result, err)
		}
	})
	ignored := []struct {
		name  string
		git   GitInput
		body  []byte
		event string
	}{
		{"other branch", GitInput{}, push("refs/heads/dev"), "push"},
		{"branch deleted", GitInput{}, []byte(`{"ref":"refs/heads/main","deleted":true}`), "push"},
		{"not a push", GitInput{}, push("refs/heads/main"), "ping"},
		{"pinned tag", GitInput{Tag: "v1.0.0"}, push("refs/heads/main"), "push"},
		{"auto deploy off", GitInput{AutoDeploy: ptr(false)}, push("refs/heads/main"), "push"},
		{"outside watch paths", GitInput{WatchPaths: []string{"deploy/**"}}, push("refs/heads/main", "docs/a.md"), "push"},
	}
	for _, c := range ignored {
		t.Run("ignores "+c.name, func(t *testing.T) {
			service, _, stack, secret := setup(t, c.git)
			header := signed(secret, c.body)
			header.Set("X-GitHub-Event", c.event)
			result, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, header, c.body)
			if err != nil || result.Deployed || result.Reason == "" {
				t.Fatalf("result %+v err %v", result, err)
			}
			if service.Operation(stack.ID).Running {
				t.Error("nothing deploys")
			}
		})
	}
	t.Run("watch path touched", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{WatchPaths: []string{"deploy/**"}})
		body := push("refs/heads/main", "deploy/app/compose.yaml")
		if result, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, signed(secret, body), body); err != nil || !result.Deployed {
			t.Fatalf("result %+v err %v", result, err)
		}
		waitOperation(t, service, stack.ID)
	})
	t.Run("frozen environment refuses", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		service.SetDeployGuard(freezeGuard{})
		body := push("refs/heads/main")
		_, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, signed(secret, body), body)
		appErr, isApp := apperr.As(err)
		if !isApp || appErr.Kind != apperr.KindConflict {
			t.Fatalf("a frozen environment must refuse with a conflict: %v", err)
		}
		if service.Operation(stack.ID).Running {
			t.Error("nothing deploys")
		}
	})
	t.Run("rotated secret invalidates the old one", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		if _, err := service.RotateWebhookSecret(ctx, stack.ID); err != nil {
			t.Fatal(err)
		}
		body := push("refs/heads/main")
		if _, err := service.HandleGitPush(ctx, stack.ID, application.ProviderGitHub, signed(secret, body), body); err == nil {
			t.Fatal("the old secret must stop working")
		}
	})
	t.Run("deploy hook with the secret", func(t *testing.T) {
		service, _, stack, secret := setup(t, GitInput{})
		if _, err := service.HandleDeployHook(ctx, stack.ID, "wrong"); err == nil {
			t.Fatal("wrong token")
		}
		if result, err := service.HandleDeployHook(ctx, stack.ID, secret); err != nil || !result.Deployed {
			t.Fatalf("%+v %v", result, err)
		}
		waitOperation(t, service, stack.ID)
	})
}

func ptr[T any](value T) *T { return &value }

func TestNonGitStacksKeepWorkingWithGitSupportEnabled(t *testing.T) {
	service, _ := newGitService(t)
	stack, err := service.Create(context.Background(), CreateInput{
		EnvironmentID: "env1", ServerID: "srv1", Name: "plain", Compose: gitCompose})
	if err != nil || stack.FromGit() {
		t.Fatalf("%+v %v", stack, err)
	}
	if _, err := service.WebhookSecret(context.Background(), stack.ID); err == nil {
		t.Error("only Git stacks have a webhook")
	}
}
