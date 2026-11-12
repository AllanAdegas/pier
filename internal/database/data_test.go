package database

import (
	"context"
	"pier/internal/notify"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/idgen"
	"pier/internal/settings"
)

func seedBackup(t *testing.T, s *Service, db Database, status BackupStatus) BackupExecution {
	t.Helper()
	b := BackupExecution{ID: idgen.New(), DatabaseID: db.ID, Status: status, StartedAt: time.Now(), FilePath: "/data/pier/backups/" + db.ID + "/test.dump"}
	if err := s.store.insertBackup(context.Background(), b); err != nil {
		t.Fatal(err)
	}
	return b
}
func conflict(t *testing.T, err error) {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok || e.Kind != apperr.KindConflict {
		t.Fatalf("want conflict, got %v", err)
	}
}

func TestBusyDatabaseRefusesBackupDeleteAndRuntimeChanges(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	seedBackup(t, s, db, BackupRunning)
	ctx := context.Background()
	for _, trigger := range []string{BackupTriggerManual, BackupTriggerScheduled} {
		_, err := s.StartBackup(ctx, db.ID, trigger)
		conflict(t, err)
	}
	conflict(t, s.Delete(ctx, db.ID, true, true))
	conflict(t, s.Start(ctx, db.ID))
	conflict(t, s.Stop(ctx, db.ID))
	if f.Ran("docker") {
		t.Fatal("busy requests must not touch the server")
	}
}
func TestAcquireIsSingleFlight(t *testing.T) {
	s, _ := newService(t)
	var wg sync.WaitGroup
	outcomes := make(chan func(), 20)
	for range 20 {
		wg.Add(1)
		go func() { defer wg.Done(); release, _ := s.acquire("db"); outcomes <- release }()
	}
	wg.Wait()
	close(outcomes)
	wins := 0
	for release := range outcomes {
		if release != nil {
			wins++
			release()
		}
	}
	if wins != 1 {
		t.Fatalf("winners=%d", wins)
	}
}
func TestFailedCompressionCleansBothPathsAndRecordsOriginalPath(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EngineMySQL)
	b := seedBackup(t, s, db, BackupRunning)
	f.On("docker ps", "container\tmain\tpostgres:17\trunning\tUp\tdatabase\tdb", 0)
	f.On("gzip --force", "disk full", 1)
	spec, _ := specFor(db.Engine)
	_, _, err := s.dump(context.Background(), db, *spec.Dump, b)
	if err == nil {
		t.Fatal("expected failure")
	}
	saved, _ := s.store.getBackup(context.Background(), b.ID)
	if saved.FilePath == "" || !f.Ran("rm -f "+saved.FilePath+" "+saved.FilePath+".gz") {
		t.Fatalf("cleanup missing, path=%s commands=%v", saved.FilePath, f.Commands)
	}
}
func TestRecoverBackupsCleansInterruptedDump(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupRunning)
	if err := s.RecoverBackups(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := s.store.getBackup(context.Background(), b.ID)
	if got.Status != BackupFailed || !f.Ran("rm -f "+b.FilePath) {
		t.Fatalf("not recovered: %+v", got)
	}
}
func TestForceCannotHideDestructionFailure(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	f.On("docker volume rm", "in use", 1)
	err := s.Delete(context.Background(), db.ID, true, true)
	if err == nil || !strings.Contains(err.Error(), "volume") {
		t.Fatalf("missing failed step: %v", err)
	}
	if _, err := s.Get(context.Background(), db.ID); err != nil {
		t.Fatal("must preserve record")
	}
}
func TestRestoreScriptsAndUnsupportedEngines(t *testing.T) {
	for _, spec := range catalog {
		script, query, ok := restoreScript(spec.Engine)
		if ok != (spec.Dump != nil) {
			t.Errorf("support mismatch %s", spec.Engine)
		}
		if ok && (script == "" || query == "") {
			t.Fatal("missing restore or query")
		}
	}
}
func TestRestoreConfirmationAndCompletedBackupRequired(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	if _, err := s.StartRestore(context.Background(), b.ID, RestoreInput{}); err == nil {
		t.Fatal("confirmation required")
	}
	if f.Ran("docker") {
		t.Fatal("no runtime changes before confirmation")
	}
}
func TestRestoreDisconnectsAndReconnectsEvenOnFailure(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	f.On("docker ps", "container\tmain\tpostgres:17\trunning\tUp\tdatabase\tdb", 0)
	f.On("pg_restore", "bad archive", 1)
	err := s.restore(context.Background(), db, db, b, Operation{ID: "restore1", Kind: "restore"}, func(string) {})
	if err == nil || !f.Ran("network disconnect pier container") || !f.Ran("network connect --alias "+db.ID+" pier container") {
		t.Fatalf("restore failure or reconnection missing: %v %v", err, f.Commands)
	}
}
func TestDisposableRestoreAlwaysRemovesContainer(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	f.On("pg_restore", "bad archive", 1)
	if err := s.restore(context.Background(), db, db, b, Operation{ID: "test1", Kind: "restore_test"}, func(string) {}); err == nil {
		t.Fatal("expected failure")
	}
	if !f.Ran("docker rm -fv pier-restore-test1") || !f.Ran("--network none --memory 1g --cpus 1") {
		t.Fatal(f.Commands)
	}
}
func TestS3CredentialsEncryptedAndAbsentFromCommandsAndResponse(t *testing.T) {
	s, f := newService(t)
	ctx := context.Background()
	cfg := settings.S3Settings{Endpoint: "https://s3.example.com", Region: "test", Bucket: "backups", AccessKey: "access-secret", SecretKey: "very-secret"}
	if err := s.UpdateS3Settings(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	var sealed string
	_ = s.store.db.QueryRow(`SELECT value FROM settings WHERE key='backup_s3'`).Scan(&sealed)
	if strings.Contains(sealed, cfg.SecretKey) {
		t.Fatal("plaintext persisted")
	}
	value, _ := s.S3Settings(ctx)
	if value.AccessKey != "" || value.SecretKey != "" {
		t.Fatal("credentials disclosed")
	}
	if err := s.s3Command(ctx, f, "/backups", "cp", "/backups/x", "s3://backups/x"); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range f.Commands {
		if strings.Contains(cmd, cfg.SecretKey) || strings.Contains(cmd, cfg.AccessKey) {
			t.Fatal("secret in argv")
		}
	}
	if !f.Ran("--env-file") || !f.Ran("rm -f /backups/.s3-") {
		t.Fatal(f.Commands)
	}
}
func TestRemoteRestoreFallbackAndRetention(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	ctx := context.Background()
	if err := s.UpdateS3Settings(ctx, settings.S3Settings{Endpoint: "https://s3.example.com", Bucket: "bucket", AccessKey: "access", SecretKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	if err := s.uploadBackup(ctx, db, b.ID, b.FilePath); err != nil {
		t.Fatal(err)
	}
	f.On("test -s", "missing", 1)
	if err := s.localBackup(ctx, b, f); err != nil {
		t.Fatal(err)
	}
	if !f.Ran("cp s3://bucket/") {
		t.Fatal("missing pull")
	}
	if err := s.DeleteBackup(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if !f.Ran("s3 rm s3://bucket/") {
		t.Fatal("remote retention missing")
	}
}
func TestRestoreTestScheduleDefaultAndValidation(t *testing.T) {
	s, _ := newService(t)
	db := create(t, s, EnginePostgreSQL)
	db.Config.Backup.RestoreTest = true
	got, err := s.Update(context.Background(), db.ID, UpdateInput{Name: db.Name, Config: db.Config})
	if err != nil {
		t.Fatal(err)
	}
	if got.Config.Backup.RestoreTestCron != "0 4 * * 0" {
		t.Fatal(got.Config.Backup)
	}
	jobs, err := s.RestoreTestSchedules(context.Background())
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs=%v err=%v", jobs, err)
	}
}

func waitOperation(t *testing.T, s *Service, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ops, err := s.ListOperations(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if len(ops) > 0 && ops[0].Status != "running" {
			return ops[0]
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return Operation{}
}
func TestTrackedRestoreAndNewDatabase(t *testing.T) {
	for _, newName := range []string{"", "restored-copy"} {
		label := newName
		if label == "" {
			label = "overwrite"
		}
		t.Run(label, func(t *testing.T) {
			s, f := newService(t)
			db := create(t, s, EnginePostgreSQL)
			b := seedBackup(t, s, db, BackupSucceeded)
			f.On("docker ps", "container\tmain\tpostgres:17\trunning\tUp\tdatabase\tdb", 0)
			op, err := s.StartRestore(context.Background(), b.ID, RestoreInput{Confirmation: db.Name, NewName: newName})
			if err != nil {
				t.Fatal(err)
			}
			done := waitOperation(t, s, op.DatabaseID)
			if done.Status != "succeeded" || done.Logs == "" || done.FinishedAt == nil {
				t.Fatalf("operation=%+v", done)
			}
			if newName != "" && op.DatabaseID == db.ID {
				t.Fatal("must create a new database")
			}
		})
	}
}
func TestBusyRestoreDoesNotCreateAnEmptyCopy(t *testing.T) {
	s, _ := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	release, _ := s.acquire(db.ID)
	defer release()
	_, err := s.StartRestore(context.Background(), b.ID, RestoreInput{Confirmation: db.Name, NewName: "copy"})
	conflict(t, err)
	dbs, _ := s.List(context.Background(), "")
	if len(dbs) != 1 {
		t.Fatal("created a database while busy")
	}
}

type capturedEvents chan notify.Event

func (c capturedEvents) Publish(event notify.Event) { c <- event }
func TestFailedRestoreTestIsTrackedAndNotified(t *testing.T) {
	s, f := newService(t)
	events := make(capturedEvents, 4)
	s.events = events
	db := create(t, s, EnginePostgreSQL)
	seedBackup(t, s, db, BackupSucceeded)
	f.On("pg_restore", "broken", 1)
	if _, err := s.StartRestoreTest(context.Background(), db.ID); err != nil {
		t.Fatal(err)
	}
	done := waitOperation(t, s, db.ID)
	if done.Status != "failed" {
		t.Fatalf("operation=%+v", done)
	}
	select {
	case event := <-events:
		if event.Type != notify.RestoreTestFailed {
			t.Fatal(event)
		}
	case <-time.After(time.Second):
		t.Fatal("missing failure notification")
	}
}
func TestRecoveryRemovesInterruptedRestoreContainerAndTemporaryFiles(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupSucceeded)
	_, err := s.store.db.Exec(`INSERT INTO database_operations(id,database_id,backup_id,kind,status,started_at) VALUES('interrupted',?,?,'restore_test','running','2026-01-01T00:00:00Z')`, db.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecoverBackups(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops, _ := s.ListOperations(context.Background(), db.ID)
	if ops[0].Status != "failed" || !f.Ran("docker rm -fv pier-restore-interrupted") || !f.Ran("test.dump.restore-interrupted") {
		t.Fatalf("recovery missing: %+v %v", ops, f.Commands)
	}
}

func TestRecoveryFailureIsRecordedWithoutPreventingStartup(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EnginePostgreSQL)
	b := seedBackup(t, s, db, BackupRunning)
	f.On("rm -f", "permission denied", 1)
	if err := s.RecoverBackups(context.Background()); err != nil {
		t.Fatalf("remote cleanup must not prevent panel startup: %v", err)
	}
	saved, err := s.store.getBackup(context.Background(), b.ID)
	if err != nil || saved.Status != BackupFailed || !strings.Contains(saved.Message, "permission denied") || saved.FilePath == "" {
		t.Fatalf("cleanup failure lost: %+v %v", saved, err)
	}
}
func TestS3FailurePreservesUsableLocalBackup(t *testing.T) {
	s, f := newService(t)
	events := make(capturedEvents, 4)
	s.events = events
	db := create(t, s, EnginePostgreSQL)
	db.Config.Backup.S3 = true
	b := seedBackup(t, s, db, BackupRunning)
	f.On("docker ps", "container\tmain\tpostgres:17\trunning\tUp\tdatabase\tdb", 0)
	f.On("wc -c", "100", 0)
	spec, _ := specFor(db.Engine)
	s.runBackup(db, *spec.Dump, b)
	saved, err := s.store.getBackup(context.Background(), b.ID)
	if err != nil || saved.Status != BackupSucceeded || saved.Message == "" {
		t.Fatalf("local backup must remain usable: %+v %v", saved, err)
	}
	select {
	case event := <-events:
		if event.Type != notify.BackupFailed {
			t.Fatal(event)
		}
	default:
		t.Fatal("missing offsite failure notification")
	}
}
