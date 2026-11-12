package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
)

func TestOperationsPolicyAndMaintenance(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	store := NewStore(db)
	if err := store.insert(ctx, Server{ID: "qa", Name: "QA", Kind: KindLocal, DataDir: "/data/pier", Proxy: DefaultProxySettings(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	o := NewOperations(db, nil)
	p, err := o.Policy(ctx, "qa")
	if err != nil || p.CleanupEnabled || p.Maintenance || p.MinAgeHours != 168 {
		t.Fatalf("defaults %+v %v", p, err)
	}
	changed := 0
	o.OnChange(func() { changed++ })
	p.Maintenance = true
	p.CleanupEnabled = true
	if _, err := o.SetPolicy(ctx, "qa", p); err != nil {
		t.Fatal(err)
	}
	if err := CheckDeployment(ctx, db, "qa"); err == nil {
		t.Fatal("maintenance did not block")
	}
	if _, err := o.Policy(ctx, "qa"); err != nil {
		t.Fatal("read blocked", err)
	}
	schedules, err := o.Schedules(ctx)
	if err != nil || len(schedules) != 1 || changed != 1 {
		t.Fatalf("schedules %v %v", schedules, err)
	}
	p.Maintenance = false
	if _, err := o.SetPolicy(ctx, "qa", p); err != nil {
		t.Fatal(err)
	}
	if err := CheckDeployment(ctx, db, "qa"); err != nil {
		t.Fatal(err)
	}
	p.MinAgeHours = 1
	if _, err := o.SetPolicy(ctx, "qa", p); err == nil {
		t.Fatal("age accepted")
	}
	p.MinAgeHours = 24
	p.CleanupCron = "garbage"
	if _, err := o.SetPolicy(ctx, "qa", p); err == nil {
		t.Fatal("cron accepted")
	}
	if _, err := o.Policy(ctx, "unknown"); err == nil {
		t.Fatal("missing server accepted")
	}
}
func TestSafeCleanupIsOptInAgedAndNonDestructiveByDefault(t *testing.T) {
	now := time.Now().UTC()
	id := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for _, tc := range []struct {
		name, kind, status, typ, stamp string
		tagged, optin, dry, want       bool
	}{
		{"dry container", "container", "exited", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, true, true},
		{"old scratch removed", "container", "exited", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, false, true},
		{"new stop protected", "container", "exited", "scratch", now.Format(time.RFC3339), false, true, false, false},
		{"running protected", "container", "running", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, false, false},
		{"backup protected", "container", "exited", "backup", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, false, false},
		{"app protected", "container", "exited", "application", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, false, false},
		{"no optin protected", "container", "exited", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), false, false, false, false},
		{"image removed", "image", "", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), false, true, false, true},
		{"tagged rollback protected", "image", "", "scratch", now.Add(-200 * time.Hour).Format(time.RFC3339), true, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := executor.NewFake()
			obj := cleanupInspect{ID: id, Created: tc.stamp}
			obj.Config.Labels = map[string]string{"pier.managed": "true", "pier.type": tc.typ}
			if tc.optin {
				obj.Config.Labels["pier.cleanup"] = "true"
			}
			obj.State.Status = tc.status
			obj.State.FinishedAt = tc.stamp
			if tc.tagged {
				obj.RepoTags = []string{"rollback:keep"}
			}
			raw, _ := json.Marshal([]cleanupInspect{obj})
			ex.On("docker "+tc.kind+" ls", id+"\nBAD; rm -rf /\n"+id, 0)
			ex.On("docker "+tc.kind+" inspect", string(raw), 0)
			report, err := cleanupEligible(context.Background(), ex, now.Add(-168*time.Hour), tc.dry)
			if err != nil {
				t.Fatal(err)
			}
			if (len(report.Candidates) == 1) != tc.want {
				t.Fatalf("candidates %+v", report)
			}
			if ex.Ran("docker "+tc.kind+" rm") != (tc.want && !tc.dry) {
				t.Fatalf("mutation mismatch %v", ex.Commands)
			}
			for _, cmd := range ex.Commands {
				if strings.Contains(cmd, "volume") || strings.Contains(cmd, "prune") || strings.Contains(cmd, "--force") || strings.Contains(cmd, "rm -rf") {
					t.Fatalf("unsafe command %s", cmd)
				}
			}
		})
	}
}
func TestSafeCleanupPropagatesFailures(t *testing.T) {
	ex := executor.NewFake().On("docker container ls", "denied", 1)
	if _, err := cleanupEligible(context.Background(), ex, time.Now(), false); err == nil {
		t.Fatal("failure swallowed")
	}
}

func TestBuildxDiagnosticWarnsWhenUnavailable(t *testing.T) {
	now := time.Now()
	checks, _ := collectChecks(context.Background(), healthyChecksFake(now).On("docker buildx version", "missing", 1), DefaultProxySettings(), func() time.Time { return now })
	last := checks[len(checks)-1]
	if last.Code != "buildx" || last.Status != "warning" || last.Fix == "" {
		t.Fatalf("%+v", last)
	}
}

func TestNetworkIOUsesExistingManagedMetricsCollection(t *testing.T) {
	ex := executor.NewFake().On("ps", "id1\tweb\tnginx\trunning\tUp 1h\tapplication\tapp1\tid", 0).On("stats", "web\t1%\t1MiB / 1GiB\t1%\t2kB / 3kB", 0)
	usage, err := readContainerUsage(context.Background(), ex)
	if err != nil || len(usage) != 1 || usage[0].NetworkIO != "2kB / 3kB" {
		t.Fatalf("usage %+v %v", usage, err)
	}
	count := 0
	for _, cmd := range ex.Commands {
		if strings.Contains(cmd, "docker stats") {
			count++
			if !strings.Contains(cmd, "NetIO") {
				t.Fatal(cmd)
			}
		}
	}
	if count != 1 {
		t.Fatalf("stats calls %d", count)
	}
}
