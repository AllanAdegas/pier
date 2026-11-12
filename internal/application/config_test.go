package application

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/apperr"
)

func gitConfig() Config {
	config := DefaultConfig(PackDockerfile)
	config.Source.Repository = "https://github.com/acme/app"
	return config
}

func fieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()
	if err == nil {
		return map[string]string{}
	}
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected a validation error, got %T: %v", err, err)
	}
	return appErr.Fields
}

func TestValidateAcceptsAndNormalizesAReasonableConfig(t *testing.T) {
	config := gitConfig()
	config.Source.Branch = "  release/1.x "
	config.Build.BaseDirectory = "services/api/"
	config.Network.Domains = []string{" app.example.com ", ""}
	config.Network.PortMappings = []string{"8080:3000", "127.0.0.1:9000:9000/udp"}
	config.Limits = Limits{CPUs: "0.5", Memory: "512m"}
	config.Storages = []Storage{{Kind: StorageVolume, Source: "data", Target: "/app/data/"}}

	if err := config.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if config.Source.Branch != "release/1.x" {
		t.Errorf("branch not trimmed: %q", config.Source.Branch)
	}
	if config.Build.BaseDirectory != "/services/api" {
		t.Errorf("base directory not normalized: %q", config.Build.BaseDirectory)
	}
	if len(config.Network.Domains) != 1 || config.Network.Domains[0] != "https://app.example.com" {
		t.Errorf("domains not canonicalized: %v", config.Network.Domains)
	}
	if config.Storages[0].Target != "/app/data" {
		t.Errorf("storage target not cleaned: %q", config.Storages[0].Target)
	}
}

// Values end up in shell commands and compose files: hostile input must be
// rejected at the boundary, field by field.
func TestValidateRejectsDangerousInput(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Config)
		field  string
	}{
		{"git ext transport runs commands", func(c *Config) { c.Source.Repository = "ext::sh -c 'id'" }, "source.repository"},
		{"local file transport", func(c *Config) { c.Source.Repository = "file:///etc" }, "source.repository"},
		{"option injection in repository", func(c *Config) { c.Source.Repository = "--upload-pack=id" }, "source.repository"},
		{"repository with shell metacharacters", func(c *Config) { c.Source.Repository = "https://x.com/a`id`" }, "source.repository"},
		{"branch starting with dash", func(c *Config) { c.Source.Branch = "-x" }, "source.branch"},
		{"branch with parent traversal", func(c *Config) { c.Source.Branch = "a/../b" }, "source.branch"},
		{"commit that is not a sha", func(c *Config) { c.Source.Commit = "HEAD; id" }, "source.commit"},
		{"ssh repository without key", func(c *Config) { c.Source.Repository = "git@github.com:acme/app.git" }, "source.private_key_id"},
		{"base directory escaping the repository", func(c *Config) { c.Build.BaseDirectory = "../../etc" }, "build.base_directory"},
		{"unknown build pack", func(c *Config) { c.Build.Pack = "buildpacks" }, "build.pack"},
		{"port out of range", func(c *Config) { c.Network.Port = 70000 }, "network.port"},
		{"domain with port", func(c *Config) { c.Network.Domains = []string{"https://x.com:8443"} }, "network.domains"},
		{"malformed port mapping", func(c *Config) { c.Network.PortMappings = []string{"80"} }, "network.port_mappings"},
		{"health path with shell metacharacters", func(c *Config) { c.Health.Path = "/x; id" }, "health.path"},
		{"cpu limit that is not a number", func(c *Config) { c.Limits.CPUs = "two" }, "limits.cpus"},
		{"memory without unit", func(c *Config) { c.Limits.Memory = "512" }, "limits.memory"},
		{"bind mount with relative source", func(c *Config) { c.Storages = []Storage{{Kind: StorageBind, Source: "data", Target: "/d"}} }, "storages.0"},
		{"mount on container root", func(c *Config) { c.Storages = []Storage{{Kind: StorageVolume, Source: "d", Target: "/"}} }, "storages.0"},
		{"volume option smuggled in target", func(c *Config) { c.Storages = []Storage{{Kind: StorageVolume, Source: "d", Target: "/d:rw"}} }, "storages.0"},
		{"duplicated mount target", func(c *Config) {
			c.Storages = []Storage{{Kind: StorageVolume, Source: "a", Target: "/d"}, {Kind: StorageVolume, Source: "b", Target: "/d"}}
		}, "storages.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			config := gitConfig()
			tc.mutate(&config)
			if _, reported := fieldsOf(t, config.Validate())[tc.field]; !reported {
				t.Errorf("expected an error on %q, got %v", tc.field, config.Validate())
			}
		})
	}
}

func TestValidateImagePack(t *testing.T) {
	config := DefaultConfig(PackImage)
	for _, image := range []string{"nginx", "nginx:1.27-alpine", "ghcr.io/acme/app:v1.2.3", "localhost:5000/app:dev"} {
		config.Source.Image = image
		if err := config.Validate(); err != nil {
			t.Errorf("image %q should be valid: %v", image, err)
		}
	}
	for _, image := range []string{"", "nginx; id", "-v /:/host alpine", "Nginx Latest", "$(id)"} {
		config.Source.Image = image
		if _, reported := fieldsOf(t, config.Validate())["source.image"]; !reported {
			t.Errorf("image %q should be rejected", image)
		}
	}
}

func TestStaticPackAlwaysServesOnPort80(t *testing.T) {
	config := gitConfig()
	config.Build.Pack = PackStatic
	config.Network.Port = 3000
	if err := config.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if config.Network.Port != 80 {
		t.Errorf("static sites are served by nginx on 80, got %d", config.Network.Port)
	}
}

func TestComposeForDescribesTheRelease(t *testing.T) {
	config := gitConfig()
	config.Network.Domains = []string{"https://app.example.com"}
	config.Network.Port = 4000
	config.Health.Enabled = true
	config.Health.Path = "/healthz"
	config.Limits = Limits{CPUs: "1.5", Memory: "1g"}
	config.Build.StartCommand = "node server.js"
	config.Storages = []Storage{{Kind: StorageVolume, Source: "data", Target: "/data"}, {Kind: StorageBind, Source: "/srv/x", Target: "/x"}}
	spec := releaseSpec{
		App:   Application{ID: "app1", Config: config},
		Image: "pier/app1:dep1", ContainerName: "app1-dep1", DeploymentID: "dep1", CommitSHA: "abc123",
		Env: map[string]string{"PORT": "9999", "API_KEY": "k"},
	}
	file, err := composeFor(spec)
	if err != nil {
		t.Fatalf("composeFor: %v", err)
	}
	service := file.Services["app1-dep1"]
	if service == nil {
		t.Fatalf("service must be named after the container, got %v", file.Services)
	}
	if service.Labels["pier.id"] != "app1" || service.Labels["pier.deployment"] != "dep1" || service.Labels["traefik.enable"] != "true" {
		t.Errorf("identification/proxy labels missing: %v", service.Labels)
	}
	// User-defined variables win over the injected ones.
	if service.Environment["PORT"] != "9999" || service.Environment["API_KEY"] != "k" {
		t.Errorf("user environment not applied: %v", service.Environment)
	}
	if service.Environment["SOURCE_COMMIT"] != "abc123" || service.Environment["PIER_URL"] != "https://app.example.com" {
		t.Errorf("injected environment missing: %v", service.Environment)
	}
	if service.Healthcheck == nil || !strings.Contains(service.Healthcheck.Test[1], "http://127.0.0.1:4000/healthz") {
		t.Errorf("healthcheck must probe the app port and path: %+v", service.Healthcheck)
	}
	if service.CPUs != "1.5" || service.MemLimit != "1g" {
		t.Errorf("limits not applied: cpus=%q mem=%q", service.CPUs, service.MemLimit)
	}
	if got := strings.Join(service.Command, " "); got != "sh -c node server.js" {
		t.Errorf("start command not applied: %q", got)
	}
	if got := strings.Join(service.Volumes, ","); got != "app1_data:/data,/srv/x:/x" {
		t.Errorf("volumes = %q", got)
	}
	if _, declared := file.Volumes["app1_data"]; !declared {
		t.Errorf("named volume must be declared at top level: %v", file.Volumes)
	}
	// The stable alias lets other resources reach the app across deployments.
	if aliases := service.Networks["pier"].Aliases; len(aliases) != 1 || aliases[0] != "app1" {
		t.Errorf("network alias = %v", aliases)
	}
}

func TestComposeForWithoutHealthcheckOrDomains(t *testing.T) {
	spec := releaseSpec{App: Application{ID: "app1", Config: gitConfig()}, Image: "img", ContainerName: "c", DeploymentID: "d"}
	file, err := composeFor(spec)
	if err != nil {
		t.Fatalf("composeFor: %v", err)
	}
	service := file.Services["c"]
	if service.Healthcheck != nil {
		t.Error("healthcheck must be omitted when disabled")
	}
	if _, published := service.Labels["traefik.enable"]; published {
		t.Error("an app without domains must not be published on the proxy")
	}
}

func TestEvaluateReadiness(t *testing.T) {
	cases := []struct {
		name       string
		inspection string
		uptime     time.Duration
		ready      bool
		fails      bool
	}{
		{"healthy", "running|healthy|0", time.Second, true, false},
		{"still starting", "running|starting|0", time.Minute, false, false},
		{"unhealthy", "running|unhealthy|0", time.Second, false, true},
		{"exited", "exited|none|0", time.Second, false, true},
		{"crash loop", "restarting|none|3", time.Second, false, true},
		{"no healthcheck, too early", "running|none|0", 2 * time.Second, false, false},
		{"no healthcheck, stable", "running|none|0", stabilizationPeriod, true, false},
		{"garbage", "???", time.Second, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ready, err := evaluateReadiness(tc.inspection, tc.uptime)
			if ready != tc.ready || (err != nil) != tc.fails {
				t.Errorf("evaluateReadiness(%q) = %v, %v", tc.inspection, ready, err)
			}
		})
	}
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhook(t *testing.T) {
	const secret, body = "s3cret", `{"ref":"refs/heads/main"}`
	header := func(key, value string) http.Header { return http.Header{key: []string{value}} }

	if !verifyWebhook(ProviderGitHub, secret, header("X-Hub-Signature-256", "sha256="+sign(secret, body)), []byte(body)) {
		t.Error("valid GitHub signature rejected")
	}
	if !verifyWebhook(ProviderGitea, secret, header("X-Gitea-Signature", sign(secret, body)), []byte(body)) {
		t.Error("valid Gitea signature rejected")
	}
	if !verifyWebhook(ProviderGitLab, secret, header("X-Gitlab-Token", secret), []byte(body)) {
		t.Error("valid GitLab token rejected")
	}
	rejected := []struct {
		provider GitProvider
		header   http.Header
	}{
		{ProviderGitHub, header("X-Hub-Signature-256", "sha256="+sign("wrong", body))},
		{ProviderGitHub, header("X-Hub-Signature-256", sign(secret, body))}, // missing "sha256=" prefix
		{ProviderGitHub, http.Header{}},
		{ProviderGitea, header("X-Gitea-Signature", "not-hex")},
		{ProviderGitLab, header("X-Gitlab-Token", "wrong")},
		{ProviderGitLab, http.Header{}},
		{"bitbucket", header("X-Hub-Signature-256", "sha256="+sign(secret, body))},
	}
	for _, tc := range rejected {
		if verifyWebhook(tc.provider, secret, tc.header, []byte(body)) {
			t.Errorf("%s accepted an invalid request: %v", tc.provider, tc.header)
		}
	}
	// A body changed after signing must not verify.
	if verifyWebhook(ProviderGitHub, secret, header("X-Hub-Signature-256", "sha256="+sign(secret, body)), []byte(body+" ")) {
		t.Error("tampered body accepted")
	}
}

func TestPushedBranch(t *testing.T) {
	if branch, ok := pushedBranch([]byte(`{"ref":"refs/heads/feature/x"}`)); !ok || branch != "feature/x" {
		t.Errorf("got %q, %v", branch, ok)
	}
	for _, body := range []string{`{"ref":"refs/tags/v1"}`, `{}`, `not json`} {
		if _, ok := pushedBranch([]byte(body)); ok {
			t.Errorf("%s must not be treated as a branch push", body)
		}
	}
}

func TestStaticDockerfile(t *testing.T) {
	spa, err := staticDockerfile(Build{PublishDirectory: "/dist", SPA: true})
	if err != nil {
		t.Fatalf("staticDockerfile: %v", err)
	}
	if !strings.Contains(spa, `COPY --exclude=.git --exclude=**/.git ["./dist/","/usr/share/nginx/html/"]`) || !strings.Contains(spa, "try_files $uri $uri/ /index.html;") {
		t.Errorf("unexpected SPA Dockerfile:\n%s", spa)
	}
	plain, _ := staticDockerfile(Build{PublishDirectory: "/"})
	if !strings.Contains(plain, `COPY --exclude=.git --exclude=**/.git ["./","/usr/share/nginx/html/"]`) || !strings.Contains(plain, "try_files $uri $uri/ =404;") {
		t.Errorf("unexpected static Dockerfile:\n%s", plain)
	}
}
