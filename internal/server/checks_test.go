package server

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"pier/internal/platform/executor"
)

func healthyChecksFake(now time.Time) *executor.Fake {
	return executor.NewFake().On("docker version", "24.0.9", 0).On("docker compose version", "2.20.0", 0).
		On("DockerRootDir", "/var/lib/docker", 0).On("df -Pk /var/lib/docker", "/dev/x 40000000 1000000 39000000 3% /var/lib/docker", 0).
		On("daemon.json", `{"log-driver":"local"}`, 0).On("date +%s", strconv.FormatInt(now.Unix(), 10), 0)
}
func TestServerChecksPassForSupportedHost(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	checks, version := collectChecks(context.Background(), healthyChecksFake(now), DefaultProxySettings(), func() time.Time { return now })
	if version != "24.0.9" || len(checks) != 6 {
		t.Fatalf("checks=%+v version=%s", checks, version)
	}
	for _, check := range checks {
		if check.Status != "pass" {
			t.Errorf("check=%+v", check)
		}
	}
}
func TestServerChecksReportActionableProblems(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	fake := healthyChecksFake(now).On("docker version", "23.0.6", 0).On("docker compose version", "v2.19.1", 0).
		On("df -Pk", "/dev/x 20000000 19000000 1000000 95% /var/lib/docker", 0).
		On("docker ps", "foreign\tnginx:1\t0.0.0.0:80->80/tcp", 0).On("daemon.json", "{}", 0).
		On("date +%s", strconv.FormatInt(now.Add(31*time.Second).Unix(), 10), 0)
	checks, _ := collectChecks(context.Background(), fake, DefaultProxySettings(), func() time.Time { return now })
	statuses := []string{"error", "error", "warning", "warning", "info", "warning"}
	for i, check := range checks {
		if check.Status != statuses[i] || check.Fix == "" {
			t.Errorf("check=%+v", check)
		}
	}
	if err := prepareCheckedServer(context.Background(), fake, Server{DataDir: "/data"}, checks); err == nil || fake.Ran("mkdir") {
		t.Fatal("unsupported Docker host prepared")
	}
}
func TestServerChecksDoNotClaimUnknownPortsOrDiskAreHealthy(t *testing.T) {
	now := time.Now()
	fake := healthyChecksFake(now).On("DockerRootDir", "", 1).On("ss -Hltn", "", 1)
	checks, _ := collectChecks(context.Background(), fake, DefaultProxySettings(), func() time.Time { return now })
	if checks[2].Status != "warning" || checks[3].Status != "warning" {
		t.Fatalf("checks=%+v", checks)
	}
}
func TestVersionMinimums(t *testing.T) {
	for _, value := range []string{"24.0.1", "25.1.0", "v24.0.0"} {
		if !versionAtLeast(value, 24, 0) {
			t.Error(value)
		}
	}
	for _, value := range []string{"23.9.0", "garbage", "24", "24.x.1"} {
		if versionAtLeast(value, 24, 0) {
			t.Error(value)
		}
	}
	if !versionAtLeast("v2.20.0", 2, 20) || versionAtLeast("2.19.9", 2, 20) {
		t.Fatal("compose minimum")
	}
}

func TestValidationPreservesHostKeyMismatchCause(t *testing.T) {
	checks := []Check{{Code: "docker", Status: "error", cause: executor.ErrHostKeyMismatch}}
	err := prepareCheckedServer(context.Background(), executor.NewFake(), Server{}, checks)
	if !errors.Is(err, executor.ErrHostKeyMismatch) {
		t.Fatalf("host key warning hidden: %v", err)
	}
}
