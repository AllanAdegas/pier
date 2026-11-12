package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
)

func operationsDB(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := NewStore(db).insert(ctx, Server{ID: "qa", Name: "QA", Kind: KindLocal, DataDir: "/data/pier", Proxy: DefaultProxySettings(), CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestOperationsPolicyAndMaintenance(t *testing.T) {
	ctx := context.Background()
	db := operationsDB(t)
	o := NewOperations(db, nil)
	p, err := o.Policy(ctx, "qa")
	if err != nil || p.CleanupEnabled || p.Maintenance || p.MinAgeHours != 168 || p.CleanupCron != "0 4 * * 0" {
		t.Fatalf("defaults %+v %v", p, err)
	}
	changed := 0
	o.OnChange(func() { changed++ })
	p.Maintenance, p.CleanupEnabled, p.CleanupCron = true, true, ""
	saved, err := o.SetPolicy(ctx, "qa", p)
	if err != nil || saved.CleanupCron != "0 4 * * 0" {
		t.Fatalf("saved %+v %v", saved, err)
	}
	if err := CheckDeployment(ctx, db, "qa"); err == nil || !strings.Contains(err.Error(), "manutenção") {
		t.Fatalf("maintenance did not block: %v", err)
	}
	if _, err := o.Policy(ctx, "qa"); err != nil {
		t.Fatal("read blocked", err)
	}
	schedules, err := o.Schedules(ctx)
	if err != nil || len(schedules) != 1 || schedules[0].ServerID != "qa" || changed != 1 {
		t.Fatalf("schedules %v %v changed=%d", schedules, err, changed)
	}
	p.Maintenance = false
	if _, err := o.SetPolicy(ctx, "qa", p); err != nil {
		t.Fatal(err)
	}
	if err := CheckDeployment(ctx, db, "qa"); err != nil {
		t.Fatal(err)
	}
	if err := CheckDeployment(ctx, db, "never-configured"); err != nil {
		t.Fatal("server without policy blocked", err)
	}
	for _, bad := range []OperationsPolicy{{MinAgeHours: 1}, {MinAgeHours: 9000}, {MinAgeHours: 24, CleanupCron: "garbage"}} {
		if _, err := o.SetPolicy(ctx, "qa", bad); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
	if changed != 2 {
		t.Fatalf("invalid policy reloaded the scheduler: %d", changed)
	}
	if _, err := o.Policy(ctx, "unknown"); err == nil {
		t.Fatal("missing server accepted")
	}
}

type fakeOwners map[string]bool

func (f fakeOwners) exists(_ context.Context, resourceType, id string) (bool, error) {
	return f[resourceType+"/"+id], nil
}

func inspected(id, name, status, stamp, typ, resource string, managed bool, tags ...string) cleanupInspect {
	object := cleanupInspect{ID: id, Name: "/" + name, Created: stamp, RepoTags: tags}
	object.Config.Labels = map[string]string{"pier.type": typ, "pier.id": resource}
	if managed {
		object.Config.Labels["pier.managed"] = "true"
	}
	object.State.Status, object.State.FinishedAt = status, stamp
	return object
}

func hexID(char string) string { return strings.Repeat(char, 64) }

func cleanupFake(t *testing.T, containers, images []cleanupInspect, builders string) *executor.Fake {
	t.Helper()
	ids := func(objects []cleanupInspect) string {
		parts := []string{"BAD; rm -rf /"}
		for _, object := range objects {
			parts = append(parts, object.ID, object.ID)
		}
		return strings.Join(parts, "\n")
	}
	containerJSON, _ := json.Marshal(containers)
	imageJSON, _ := json.Marshal(images)
	return executor.NewFake().
		On("docker container ls", ids(containers), 0).On("docker container inspect", string(containerJSON), 0).
		On("docker image ls", ids(images), 0).On("docker image inspect", string(imageJSON), 0).
		On("docker buildx ls", builders, 0).
		On("docker buildx du", "ID\tRECLAIMABLE\nReclaimable:   1.2GB\nTotal:   1.5GB", 0).
		On("docker buildx prune", "Total:\t1.2GB", 0)
}

func TestSafeCleanupRemovesOnlyAgedPierObjectsWithoutOwner(t *testing.T) {
	now := time.Now().UTC()
	old, young := now.Add(-200*time.Hour).Format(time.RFC3339Nano), now.Add(-time.Hour).Format(time.RFC3339Nano)
	containers := []cleanupInspect{
		inspected(hexID("a"), "orphan", "exited", old, "application", "deleted-app", true),
		inspected(hexID("b"), "helper", "dead", old, "backup", "x", true),
		inspected(hexID("c"), "stopped-app", "exited", old, "application", "live-app", true),
		inspected(hexID("d"), "pier-proxy", "exited", old, "proxy", "proxy", true),
		inspected(hexID("e"), "recent", "exited", young, "application", "deleted-app", true),
		inspected(hexID("f"), "running", "running", old, "application", "deleted-app", true),
		inspected(hexID("0"), "foreign", "exited", old, "", "", false),
	}
	images := []cleanupInspect{
		inspected("sha256:"+hexID("1"), "", "", old, "", "app", true),
		inspected("sha256:"+hexID("2"), "", "", old, "", "app", true, "app:rollback"),
		inspected("sha256:"+hexID("3"), "", "", young, "", "app", true),
		inspected("sha256:"+hexID("4"), "", "", old, "", "", false),
	}
	builders := "default\npier-build-0123456789abcdef\nmybuilder\npier-build-0123456789abcdef"
	for _, dryRun := range []bool{true, false} {
		ex := cleanupFake(t, containers, images, builders)
		sweep := cleanupSweep{ex: ex, owners: fakeOwners{"application/live-app": true}, before: now.Add(-168 * time.Hour), ageHours: 168, dryRun: dryRun}
		report, err := sweep.run(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		got := []string{}
		for _, item := range report.Candidates {
			got = append(got, item.Kind+":"+item.ID)
		}
		want := []string{"container:" + hexID("a"), "container:" + hexID("b"), "image:sha256:" + hexID("1"), "build_cache:pier-build-0123456789abcdef"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("dry=%v candidates\n got %v\nwant %v", dryRun, got, want)
		}
		if len(report.Kept) != 2 || report.Kept[0].Name != "stopped-app" || report.Kept[1].Name != "pier-proxy" || report.Kept[0].Reason == "" {
			t.Fatalf("kept %+v", report.Kept)
		}
		mutating := []string{"docker container rm", "docker image rm", "docker buildx prune"}
		for _, fragment := range mutating {
			if ex.Ran(fragment) == dryRun {
				t.Fatalf("dry=%v ran %q: %v", dryRun, fragment, ex.Commands)
			}
		}
		if !dryRun && (len(report.Removed) != 4 || report.Removed[3].Detail != "Total: 1.2GB") {
			t.Fatalf("removed %+v", report.Removed)
		}
		if dryRun && report.Candidates[3].Detail != "Reclaimable: 1.2GB" {
			t.Fatalf("build cache estimate %+v", report.Candidates[3])
		}
		for _, command := range ex.Commands {
			for _, unsafe := range []string{"volume", "system prune", " -f ", "--force " + hexID("a"), " -v ", "rm -rf", "BAD", hexID("c"), hexID("0"), hexID("2"), "mybuilder", "builder default"} {
				if strings.Contains(command+" ", unsafe) && !strings.Contains(command, "inspect") {
					t.Fatalf("unsafe command %q (%q)", command, unsafe)
				}
			}
			if strings.Contains(command, "buildx prune") && !strings.Contains(command, "until=168h") {
				t.Fatalf("build cache prune without age filter: %s", command)
			}
		}
	}
}

func TestSafeCleanupReportsRemovalFailuresAndPropagatesListingFailures(t *testing.T) {
	old := time.Now().Add(-200 * time.Hour).UTC().Format(time.RFC3339Nano)
	ex := cleanupFake(t, nil, []cleanupInspect{inspected("sha256:"+hexID("1"), "", "", old, "", "app", true)}, "").
		On("docker image rm", "image is being used by stopped container", 1)
	sweep := cleanupSweep{ex: ex, owners: fakeOwners{}, before: time.Now().Add(-168 * time.Hour), ageHours: 168}
	report, err := sweep.run(context.Background())
	if err != nil || len(report.Failed) != 1 || len(report.Removed) != 0 || !strings.Contains(report.Failed[0].Reason, "being used") {
		t.Fatalf("report %+v err %v", report, err)
	}
	failing := executor.NewFake().On("docker container ls", "denied", 1)
	sweep = cleanupSweep{ex: failing, owners: fakeOwners{}, before: time.Now()}
	if _, err := sweep.run(context.Background()); err == nil {
		t.Fatal("listing failure swallowed")
	}
	noBuildx := cleanupFake(t, nil, nil, "").On("docker buildx ls", "unknown command", 1)
	sweep = cleanupSweep{ex: noBuildx, owners: fakeOwners{}, before: time.Now()}
	report, err = sweep.run(context.Background())
	if err != nil || len(report.Kept) != 1 || report.Kept[0].Kind != CleanupBuildCache {
		t.Fatalf("missing buildx %+v %v", report, err)
	}
}

func TestResourceOwnersReadPierTables(t *testing.T) {
	ctx := context.Background()
	db := operationsDB(t)
	for _, query := range []string{
		`INSERT INTO projects(id,name,created_at) VALUES('p','P','2026-01-01')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES('e','p','prod','2026-01-01')`,
		`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at) VALUES('app','e','qa','api','{}','s','2026-01-01','2026-01-01')`,
	} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	owners := resourceOwners{db: db}
	for _, tc := range []struct {
		typ, id string
		want    bool
	}{{"application", "app", true}, {"application", "gone", false}, {"stack", "app", false}, {"database", "x", false}, {"backup", "x", false}, {"application", "", true}} {
		got, err := owners.exists(ctx, tc.typ, tc.id)
		if err != nil || got != tc.want {
			t.Fatalf("%s/%s = %v %v", tc.typ, tc.id, got, err)
		}
	}
}

type staticConnector struct{ ex executor.Executor }

func (c staticConnector) Executor(context.Context, string) (executor.Executor, Server, error) {
	return c.ex, Server{ID: "qa", Name: "QA"}, nil
}

func TestOperationsCleanupUsesPolicyAgeAndKeepsLastReport(t *testing.T) {
	ctx := context.Background()
	db := operationsDB(t)
	now := time.Now().UTC()
	stamp := now.Add(-30 * time.Hour).Format(time.RFC3339Nano)
	ex := cleanupFake(t, []cleanupInspect{inspected(hexID("a"), "orphan", "exited", stamp, "application", "gone", true)}, nil, "")
	o := NewOperations(db, staticConnector{ex})
	o.now = func() time.Time { return now }
	if _, found, _ := o.LastCleanup(ctx, "qa"); found {
		t.Fatal("report before any cleanup")
	}
	report, err := o.Cleanup(ctx, "qa", true)
	if err != nil || len(report.Candidates) != 0 || report.MinAgeHours != 168 {
		t.Fatalf("default age removed a 30h container: %+v %v", report, err)
	}
	policy := DefaultOperationsPolicy()
	policy.MinAgeHours = 24
	if _, err := o.SetPolicy(ctx, "qa", policy); err != nil {
		t.Fatal(err)
	}
	report, err = o.Cleanup(ctx, "qa", false)
	if err != nil || len(report.Removed) != 1 || report.FinishedAt.IsZero() {
		t.Fatalf("%+v %v", report, err)
	}
	last, found, err := o.LastCleanup(ctx, "qa")
	if err != nil || !found || len(last.Removed) != 1 {
		t.Fatalf("last %+v %v %v", last, found, err)
	}
	if !o.begin("qa") {
		t.Fatal("begin")
	}
	if _, err := o.Cleanup(ctx, "qa", true); err == nil {
		t.Fatal("concurrent cleanup accepted")
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
}
