package application

import (
	"context"
	"net/netip"
	"strings"
	"testing"
)

func autoDomainFixture(t *testing.T, config Config) (*deployFixture, *Service) {
	t.Helper()
	fixture := newDeployFixture(t, config)
	fixture.ex.On("api.ipify.org", "203.0.113.7\n", 0)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	return fixture, NewService(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil)
}

func withoutDomains() Config {
	config := imageConfig()
	config.Network.Domains = []string{}
	return config
}

func TestAutoDomainLabelIsDNSSafeAndBounded(t *testing.T) {
	if got := autoDomainLabel("Minha API_v2!", "Produção", "ab12cd"); got != "minha-api-v2-produ-o-ab12cd" {
		t.Errorf("label = %s", got)
	}
	long := autoDomainLabel(strings.Repeat("x", 100), "staging", "ab12cd")
	if len(long) > maxDomainLabel || !strings.HasSuffix(long, "-staging-ab12cd") {
		t.Errorf("long label = %s (%d)", long, len(long))
	}
	host, err := autoDomainHost("app-prod-ab12cd", "", netip.MustParseAddr("203.0.113.7"))
	if err != nil || host != "app-prod-ab12cd.203.0.113.7.sslip.io" {
		t.Errorf("sslip host = %s %v", host, err)
	}
	if _, err := autoDomainHost("x", "", netip.MustParseAddr("2001:db8::1")); err == nil {
		t.Error("sslip.io needs IPv4")
	}
	if host, _ := autoDomainHost("x", "apps.example.com", netip.Addr{}); host != "x.apps.example.com" {
		t.Errorf("base host = %s", host)
	}
}

func TestAutoDomainSettingsValidation(t *testing.T) {
	_, service := autoDomainFixture(t, withoutDomains())
	ctx := context.Background()
	if _, err := service.SetAutoDomainSettings(ctx, AutoDomainSettings{Enabled: true, HTTPS: true}); err == nil {
		t.Error("HTTPS on sslip.io must be refused")
	}
	if _, err := service.SetAutoDomainSettings(ctx, AutoDomainSettings{BaseDomain: "not a domain"}); err == nil {
		t.Error("invalid base domain accepted")
	}
	saved, err := service.SetAutoDomainSettings(ctx, AutoDomainSettings{Enabled: true, BaseDomain: "*.Apps.Example.com", HTTPS: true})
	if err != nil || saved.BaseDomain != "apps.example.com" {
		t.Fatalf("saved = %+v %v", saved, err)
	}
	if loaded, _ := service.AutoDomainSettings(ctx); loaded != saved {
		t.Errorf("loaded %+v, saved %+v", loaded, saved)
	}
}

func TestAutoDomainIsAssignedOnDeployOverHTTPAndRemovable(t *testing.T) {
	fixture, service := autoDomainFixture(t, withoutDomains())
	ctx := context.Background()
	if deployment := fixture.deploy(t); deployment.Status != DeploymentFinished {
		t.Fatalf("deploy: %s", deployment.Error)
	}
	if app, _ := service.Get(ctx, fixture.app.ID); len(app.Config.Network.Domains) != 0 {
		t.Fatal("setting off must not add a domain")
	}
	if _, err := service.SetAutoDomainSettings(ctx, AutoDomainSettings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.deploy(t)
	app, _ := service.Get(ctx, fixture.app.ID)
	expected := "http://" + autoDomainLabel("demo", "production", strings.ToLower(fixture.app.ID[:6])) + ".203.0.113.7.sslip.io"
	if deployment.Status != DeploymentFinished || len(app.Config.Network.Domains) != 1 || app.Config.Network.Domains[0] != expected {
		t.Fatalf("domains = %v (want %s); %s", app.Config.Network.Domains, expected, deployment.Error)
	}
	if !strings.Contains(fixture.logText(t, deployment.ID), expected) {
		t.Error("the deployment log should name the automatic domain")
	}
	record, err := service.AutoDomain(ctx, fixture.app.ID)
	if err != nil || !record.Automatic || record.URL != expected {
		t.Fatalf("record = %+v %v", record, err)
	}
	if app, err = service.RemoveAutoDomain(ctx, fixture.app.ID); err != nil || len(app.Config.Network.Domains) != 0 {
		t.Fatalf("remove: %v %v", app.Config.Network.Domains, err)
	}
	fixture.deploy(t)
	if app, _ = service.Get(ctx, fixture.app.ID); len(app.Config.Network.Domains) != 0 {
		t.Fatal("a removed automatic domain came back")
	}
	if app, err = service.GenerateAutoDomain(ctx, fixture.app.ID); err != nil || len(app.Config.Network.Domains) != 1 {
		t.Fatalf("explicit generation after removal: %v %v", app.Config.Network.Domains, err)
	}
}

func TestAutoDomainRemovedThroughConfigEditIsNotReAdded(t *testing.T) {
	fixture, service := autoDomainFixture(t, withoutDomains())
	ctx := context.Background()
	if _, err := service.SetAutoDomainSettings(ctx, AutoDomainSettings{Enabled: true, BaseDomain: "apps.example.com", HTTPS: true}); err != nil {
		t.Fatal(err)
	}
	fixture.deploy(t)
	app, _ := service.Get(ctx, fixture.app.ID)
	if len(app.Config.Network.Domains) != 1 || !strings.HasPrefix(app.Config.Network.Domains[0], "https://demo-production-") ||
		!strings.HasSuffix(app.Config.Network.Domains[0], ".apps.example.com") {
		t.Fatalf("base-domain automatic domain = %v", app.Config.Network.Domains)
	}
	app.Config.Network.Domains = []string{}
	if _, err := service.Update(ctx, app.ID, UpdateInput{Name: app.Name, Config: app.Config}); err != nil {
		t.Fatal(err)
	}
	fixture.deploy(t)
	if app, _ = service.Get(ctx, fixture.app.ID); len(app.Config.Network.Domains) != 0 {
		t.Fatalf("domain removed in the form came back: %v", app.Config.Network.Domains)
	}
	if record, _ := service.AutoDomain(ctx, app.ID); !record.Removed || record.Automatic {
		t.Fatalf("record should be removed: %+v", record)
	}
}

func TestAutoDomainAvoidsCollisions(t *testing.T) {
	fixture, service := autoDomainFixture(t, withoutDomains())
	ctx := context.Background()
	settings := AutoDomainSettings{Enabled: true, BaseDomain: "apps.example.com"}
	first := autoDomainCandidates(fixture.app, "production")[0] + ".apps.example.com"
	taken := imageConfig()
	taken.Network.Domains = []string{"https://" + first}
	if _, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "other", Config: taken}); err != nil {
		t.Fatal(err)
	}
	host, err := fixture.store.pickAutoDomain(ctx, fixture.app, "production", settings, netip.Addr{})
	if err != nil || host == first || !strings.HasSuffix(host, ".apps.example.com") {
		t.Fatalf("collision not avoided: %s %v", host, err)
	}
	if err := fixture.store.saveAutoDomain(ctx, fixture.app.ID, host); err != nil {
		t.Fatal(err)
	}
	other, _ := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "demo", Config: withoutDomains()})
	reserved, err := fixture.store.domainsInUse(ctx, other.ID)
	if err != nil || !reserved[host] || !reserved[first] {
		t.Fatalf("reserved = %v %v", reserved, err)
	}
}

func TestAutoDomainSkipsWorkerOnlyApplications(t *testing.T) {
	config := withoutDomains()
	config.Processes = []Process{{Name: "worker", Command: "run"}}
	if servesWeb(config) {
		t.Fatal("worker-only application cannot receive a domain")
	}
	if !servesWeb(withoutDomains()) || servesWeb(hostNetworkConfig()) {
		t.Fatal("servesWeb is wrong for a default or host-network application")
	}
}
