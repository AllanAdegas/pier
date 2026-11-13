package volumebackup

import (
	"context"
	"strings"
	"testing"
	"time"

	"pier/internal/notify"
	"pier/internal/platform/apperr"
)

func TestManualBackupArchivesIncludedMountsReadOnly(t *testing.T) {
	f := newFixture(t)
	backup := f.backupAndWait(t)
	if backup.Status != StatusSucceeded || backup.SizeBytes != 2048 {
		t.Fatalf("backup: %+v", backup)
	}
	if len(backup.Mounts) != 2 {
		t.Fatalf("only the volume and the in-data-dir bind belong to the archive: %+v", backup.Mounts)
	}
	run := commandsContaining(f.fake, "tar -czf")
	if len(run) != 1 || !strings.Contains(run[0], "readonly") || !strings.Contains(run[0], "pier-volbackup-"+backup.ID) {
		t.Fatalf("helper command: %v", run)
	}
	if strings.Contains(run[0], "/srv/shared") || strings.Contains(run[0], "docker.sock") {
		t.Errorf("excluded mounts leaked into the archive: %s", run[0])
	}
	if !f.fake.Ran("mkdir -p /data/pier/backups/volumes-app1") || !f.fake.Ran("gzip -t") {
		t.Errorf("directory preparation or verification missing: %v", f.fake.Commands)
	}
	if f.fake.Ran("docker stop") || f.fake.Ran("aws-cli") {
		t.Errorf("no downtime and no S3 by default: %v", f.fake.Commands)
	}
}

func TestBackupWithoutPersistentDataIsRefused(t *testing.T) {
	f := newFixture(t)
	f.fake.On("docker inspect", `[{"Type":"tmpfs","Destination":"/t"}]`, 0)
	_, err := f.service.Start(context.Background(), KindApplication, "app1", TriggerManual)
	if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindConflict {
		t.Fatalf("expected a conflict, got %v", err)
	}
}

func TestFailedBackupCleansUpAndPublishesAlertWithResourceType(t *testing.T) {
	f := newFixture(t)
	f.fake.On("tar -czf", "no space left", 1)
	backup := f.backupAndWait(t)
	if backup.Status != StatusFailed || backup.Message == "" {
		t.Fatalf("backup: %+v", backup)
	}
	if !f.fake.Ran("docker rm -fv pier-volbackup-"+backup.ID) || !f.fake.Ran("rm -f "+backup.FilePath) {
		t.Errorf("helper and partial file must be removed: %v", f.fake.Commands)
	}
	events := f.events.all()
	if len(events) != 1 || events[0].Type != notify.BackupFailed || events[0].ResourceType != KindApplication ||
		events[0].Resource != "My App" {
		t.Fatalf("events: %+v", events)
	}
}

func TestSuccessfulBackupPublishesSucceeded(t *testing.T) {
	f := newFixture(t)
	f.backupAndWait(t)
	events := f.events.all()
	if len(events) != 1 || events[0].Type != notify.BackupSucceeded || events[0].ResourceType != KindApplication {
		t.Fatalf("events: %+v", events)
	}
}

func TestQuiesceStopsAndStartsTheContainersAroundTheArchive(t *testing.T) {
	f := newFixture(t)
	if _, err := f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{Quiesce: true}); err != nil {
		t.Fatal(err)
	}
	f.backupAndWait(t)
	stop, run, start := indexOfCommand(f.fake, "docker stop"), indexOfCommand(f.fake, "tar -czf"), indexOfCommand(f.fake, "docker start")
	if stop < 0 || !(stop < run && run < start) {
		t.Fatalf("expected stop < archive < start, got %d %d %d: %v", stop, run, start, f.fake.Commands)
	}
}

func TestQuiescedContainersStartAgainWhenTheArchiveFails(t *testing.T) {
	f := newFixture(t)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{Quiesce: true})
	f.fake.On("tar -czf", "boom", 1)
	f.backupAndWait(t)
	if !f.fake.Ran("docker start") {
		t.Fatalf("the resource must come back after a failure: %v", f.fake.Commands)
	}
}

func TestS3CopyReusesTheDatabaseOffsitePath(t *testing.T) {
	f := newFixture(t)
	f.configureS3(t)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{S3: true})
	backup := f.backupAndWait(t)
	upload := commandsContaining(f.fake, "amazon/aws-cli:2.22.35")
	if len(upload) != 1 || !strings.Contains(upload[0], "--endpoint-url https://s3.example.com s3 cp") ||
		!strings.Contains(upload[0], "s3://bucket/pre/volumes/app1/") {
		t.Fatalf("upload: %v", upload)
	}
	if strings.Contains(upload[0], "SK") || strings.Contains(upload[0], "AK") {
		t.Errorf("credentials must stay out of argv: %s", upload[0])
	}
	stored, _ := f.service.store.backup(context.Background(), backup.ID)
	if !stored.Remote || !strings.HasPrefix(stored.remoteKey, "s3://bucket/pre/volumes/app1/") {
		t.Errorf("remote key: %q", stored.remoteKey)
	}
}

func TestFailedS3CopyKeepsTheLocalBackup(t *testing.T) {
	f := newFixture(t)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{S3: true})
	backup := f.backupAndWait(t)
	if backup.Status != StatusSucceeded || !strings.Contains(backup.Message, "S3") {
		t.Fatalf("backup: %+v", backup)
	}
	kinds := []notify.EventType{}
	for _, event := range f.events.all() {
		kinds = append(kinds, event.Type)
	}
	if len(kinds) != 2 || kinds[0] != notify.BackupFailed {
		t.Errorf("events: %v", kinds)
	}
}

func TestRetentionKeepsOnlyTheNewestSuccessfulBackups(t *testing.T) {
	f := newFixture(t)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{Retention: 2})
	var first Backup
	for i := 0; i < 4; i++ {
		b := f.backupAndWait(t)
		if i == 0 {
			first = b
		}
		time.Sleep(2 * time.Millisecond)
	}
	list, err := f.service.List(context.Background(), KindApplication, "app1")
	if err != nil || len(list) != 2 {
		t.Fatalf("retention: %d backups, %v", len(list), err)
	}
	if !f.fake.Ran("rm -f " + first.FilePath) {
		t.Errorf("the oldest archive must be removed from the server")
	}
}

func TestBackupIsRefusedWhileADeployOrAnotherOperationRuns(t *testing.T) {
	f := newFixture(t)
	f.resources.setBusy(apperr.Conflict("Aguarde o deploy da aplicação terminar."))
	if _, err := f.service.Start(context.Background(), KindApplication, "app1", TriggerManual); err == nil {
		t.Fatal("a running deploy must block the backup")
	}
	f.resources.setBusy(nil)
	release, err := f.service.acquire(KindApplication, "app1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Start(context.Background(), KindApplication, "app1", TriggerManual); err == nil {
		t.Fatal("single-flight must refuse a second operation")
	}
	release()
}

func TestRunningWorkPersistedInTheTableBlocksAdmission(t *testing.T) {
	f := newFixture(t)
	_ = f.service.store.insertBackup(context.Background(), Backup{ID: "old", ResourceType: KindApplication,
		ResourceID: "app1", Status: StatusRunning, StartedAt: time.Now(), Trigger: TriggerManual})
	if _, err := f.service.Start(context.Background(), KindApplication, "app1", TriggerManual); err == nil {
		t.Fatal("a persisted running backup must block a new one")
	}
}

func TestUnknownResourcesAndKindsAreNotFound(t *testing.T) {
	f := newFixture(t)
	for _, call := range []func() error{
		func() error { _, err := f.service.Start(context.Background(), KindApplication, "nope", TriggerManual); return err },
		func() error { _, err := f.service.Mounts(context.Background(), "database", "app1"); return err },
		func() error { _, err := f.service.List(context.Background(), "x", "app1"); return err },
	} {
		if appErr, ok := apperr.As(call()); !ok || appErr.Kind != apperr.KindNotFound {
			t.Errorf("expected not found")
		}
	}
}

func TestPolicyValidation(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.SetPolicy(ctx, KindApplication, "app1", Policy{Enabled: true, Cron: "not cron"}); err == nil {
		t.Error("invalid cron must be rejected")
	}
	if _, err := f.service.SetPolicy(ctx, KindApplication, "app1", Policy{Retention: 1000}); err == nil {
		t.Error("retention above the maximum must be rejected")
	}
	saved, err := f.service.SetPolicy(ctx, KindApplication, "app1", Policy{Enabled: true})
	if err != nil || saved.Cron != defaultCron || saved.Retention != defaultRetention || saved.Quiesce {
		t.Errorf("defaults: %+v %v", saved, err)
	}
	loaded, _ := f.service.Policy(ctx, KindApplication, "app1")
	if loaded != saved {
		t.Errorf("round trip: %+v != %+v", loaded, saved)
	}
	schedules, _ := f.service.Schedules(ctx)
	if len(schedules) != 1 || schedules[0].ID != "app1" || schedules[0].Cron != defaultCron {
		t.Errorf("schedules: %+v", schedules)
	}
}

func TestDeletingAnArchiveRemovesFileAndRemoteObject(t *testing.T) {
	f := newFixture(t)
	f.configureS3(t)
	_, _ = f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{S3: true})
	backup := f.backupAndWait(t)
	if err := f.service.Delete(context.Background(), backup.ID); err != nil {
		t.Fatal(err)
	}
	if !f.fake.Ran("rm -f "+backup.FilePath) || !f.fake.Ran("s3 rm s3://bucket/pre/volumes/app1/") {
		t.Errorf("commands: %v", f.fake.Commands)
	}
	if _, err := f.service.store.backup(context.Background(), backup.ID); err == nil {
		t.Error("record must be gone")
	}
}

func TestOpenStreamsTheArchive(t *testing.T) {
	f := newFixture(t)
	backup := f.backupAndWait(t)
	f.fake.Files[backup.FilePath] = []byte("tar-bytes")
	reader, _, err := f.service.Open(context.Background(), backup.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader.Close()
	failed, _ := f.service.store.backup(context.Background(), backup.ID)
	_ = f.service.store.finishBackup(context.Background(), failed.ID, StatusFailed, 0, "x")
	if _, _, err := f.service.Open(context.Background(), backup.ID); err == nil {
		t.Error("a failed backup has no file to open")
	}
}
