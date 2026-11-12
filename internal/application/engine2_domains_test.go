package application

import (
	"context"
	"errors"
	"strings"
	"testing"

	"pier/internal/server"
)

type fakeDomains map[string]server.DomainCheck

func (f fakeDomains) CheckDomain(_ context.Context, _ string, domain string) (server.DomainCheck, error) {
	result, known := f[domain]
	if !known {
		return server.DomainCheck{}, errors.New("resolver unavailable")
	}
	return result, nil
}

func TestADomainNotPointingToTheServerIsPublishedWithoutCertificate(t *testing.T) {
	config := imageConfig()
	config.Network.Domains = []string{"https://ready.example.com", "https://pending.example.com", "https://unknown.example.com"}
	fixture := newDeployFixture(t, config)
	fixture.deployer.SetDomainChecker(fakeDomains{
		"ready.example.com":   {OK: true},
		"pending.example.com": {OK: false, RecordHint: "Crie um registro A pending.example.com → 203.0.113.10."},
	})
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)

	deployment := fixture.deploy(t)

	if deployment.Status != DeploymentFinished {
		t.Fatalf("a DNS problem must not fail the deployment: %s %s", deployment.Status, deployment.Error)
	}
	compose := string(fixture.ex.Files["/data/pier/applications/"+fixture.app.ID+"/docker-compose.yaml"])
	if strings.Count(compose, "certresolver") != 2 {
		t.Errorf("only ready and unverifiable domains request certificates:\n%s", compose)
	}
	if !strings.Contains(compose, "PIER_URL: https://ready.example.com") {
		t.Errorf("runtime URL must keep the configured scheme:\n%s", compose)
	}
	if len(deployment.Hints) != 1 || deployment.Hints[0].Code != "dns-not-pointing" ||
		!strings.Contains(deployment.Hints[0].Fix, "203.0.113.10") {
		t.Errorf("hints = %+v", deployment.Hints)
	}
	if log := fixture.logText(t, deployment.ID); !strings.Contains(log, "pending.example.com ainda não aponta") {
		t.Errorf("log does not explain the HTTP-only route:\n%s", log)
	}
}

func TestPendingCertificatesOnlyDowngradeHTTPSRoutesOfThePendingHost(t *testing.T) {
	routes, err := parseRoutes([]string{"https://a.example.com/api", "http://b.example.com", "https://b.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	published := withoutPendingCertificates(routes, map[string]bool{"a.example.com": true})
	if published[0].Scheme != "http" || published[1].Scheme != "http" || published[2].Scheme != "https" || routes[0].Scheme != "https" {
		t.Errorf("published = %+v, original = %+v", published, routes)
	}
}
