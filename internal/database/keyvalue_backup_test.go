package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
)

const keyValuePs = "container\tmain\timg\trunning\tUp\tdatabase\t"

func startKeyValue(t *testing.T, engine Engine) (*Service, Database, *executor.Fake) {
	t.Helper()
	s, f := newService(t)
	db := create(t, s, engine)
	f.On("docker ps", keyValuePs+db.ID, 0).On("wc -c", "512", 0)
	return s, db, f
}

func waitBackup(t *testing.T, s *Service, id string) BackupExecution {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		b, _ := s.store.getBackup(context.Background(), id)
		if b.Status != BackupRunning {
			s.mu.Lock()
			for len(s.active) > 0 {
				s.mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				s.mu.Lock()
			}
			s.mu.Unlock()
			return b
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("backup did not finish")
	return BackupExecution{}
}

func TestKeyValueEnginesDeclareASaveCommandWithoutSecretsInArgv(t *testing.T) {
	cases := map[Engine]struct{ contains []string }{
		EngineRedis:     {[]string{"cli=redis-cli", "BGSAVE", "rdb_bgsave_in_progress", "cat /data/dump.rdb"}},
		EngineKeyDB:     {[]string{"cli=keydb-cli", "BGSAVE", "cat /data/dump.rdb"}},
		EngineDragonfly: {[]string{"redis-cli SAVE RDB pier-backup", "cat \"$file\""}},
	}
	for engine, want := range cases {
		spec, _ := specFor(engine)
		if spec.Dump == nil || spec.Dump.Extension != "rdb" || spec.Dump.Compress {
			t.Fatalf("%s: dump spec %+v", engine, spec.Dump)
		}
		for _, fragment := range want.contains {
			if !strings.Contains(spec.Dump.Script, fragment) {
				t.Errorf("%s: script lacks %q:\n%s", engine, fragment, spec.Dump.Script)
			}
		}
		if strings.Contains(spec.Dump.Script, "-a ") || strings.Contains(spec.Dump.Script, "--pass") ||
			strings.Contains(spec.Dump.Script, "requirepass") {
			t.Errorf("%s: the password must come from REDISCLI_AUTH, not argv", engine)
		}
		if spec.Environment(Credentials{Password: "pw"})["REDISCLI_AUTH"] != "pw" {
			t.Errorf("%s: REDISCLI_AUTH is what authenticates the save", engine)
		}
	}
}

func TestKeyValueBackupRunsThePerEngineSaveInsideTheContainer(t *testing.T) {
	for _, engine := range []Engine{EngineRedis, EngineKeyDB, EngineDragonfly} {
		s, db, f := startKeyValue(t, engine)
		backup, err := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
		if err != nil {
			t.Fatalf("%s: %v", engine, err)
		}
		done := waitBackup(t, s, backup.ID)
		if done.Status != BackupSucceeded || !strings.HasSuffix(done.FilePath, ".rdb") {
			t.Fatalf("%s: %+v", engine, done)
		}
		spec, _ := specFor(engine)
		if !f.Ran("docker exec container sh -c") || !f.Ran("> "+done.FilePath) {
			t.Errorf("%s: save+copy not run: %v", engine, f.Commands)
		}
		expected := "keydb-cli"
		if engine == EngineRedis {
			expected = "redis-cli"
		}
		if engine != EngineDragonfly && !strings.Contains(spec.Dump.Script, "cli="+expected) {
			t.Errorf("%s: wrong client", engine)
		}
		if f.Ran("gzip") {
			t.Errorf("%s: an RDB is stored as is", engine)
		}
	}
}

func TestKeyValueBackupFailureIsReportedAndCleansTheFile(t *testing.T) {
	s, db, f := startKeyValue(t, EngineRedis)
	f.On("docker exec", "ERR Background save already in progress", 1)
	backup, _ := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
	done := waitBackup(t, s, backup.ID)
	if done.Status != BackupFailed || done.Message == "" || !f.Ran("rm -f "+done.FilePath) {
		t.Fatalf("backup: %+v", done)
	}
}

func TestKeyValueBackupCanBeScheduledButNotTestedByRestore(t *testing.T) {
	s, _ := newService(t)
	ctx := context.Background()
	db := create(t, s, EngineRedis)
	config := db.Config
	config.Backup = BackupPolicy{Enabled: true, Cron: "0 3 * * *", Retention: 3}
	if _, err := s.Update(ctx, db.ID, UpdateInput{Name: "x", Config: config}); err != nil {
		t.Fatalf("scheduled backup must be accepted: %v", err)
	}
	config.Backup.RestoreTest = true
	if _, err := s.Update(ctx, db.ID, UpdateInput{Name: "x", Config: config}); err == nil {
		t.Error("the disposable restore test does not apply to key-value engines")
	}
	schedules, _ := s.BackupSchedules(ctx)
	if len(schedules) != 1 {
		t.Errorf("schedules: %+v", schedules)
	}
}

func TestClickHouseStillRefusesBackupWithAClearConflict(t *testing.T) {
	s, f := newService(t)
	db := create(t, s, EngineClickHouse)
	_, err := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
	conflict(t, err)
	if !strings.Contains(err.Error(), "ClickHouse") || f.Ran("docker") {
		t.Errorf("message %q / commands %v", err, f.Commands)
	}
	infos := Engines(i18n.Portuguese)
	for _, info := range infos {
		want := info.Engine != EngineClickHouse
		if info.SupportsBackup != want {
			t.Errorf("%s supports_backup=%v", info.Engine, info.SupportsBackup)
		}
	}
}

func TestKeyValueRestoreReplacesTheDatasetInTheStoppedContainersVolume(t *testing.T) {
	s, db, f := startKeyValue(t, EngineRedis)
	backup, _ := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
	done := waitBackup(t, s, backup.ID)
	f.Commands = nil
	op, err := s.StartRestore(context.Background(), done.ID, RestoreInput{Confirmation: db.Name})
	if err != nil {
		t.Fatal(err)
	}
	waitKeyValueOperation(t, s, db.ID, op.ID)
	stop, helper, start := -1, -1, -1
	for i, command := range f.Commands {
		switch {
		case strings.Contains(command, "docker stop"):
			stop = i
		case strings.Contains(command, "pier-restore-"+op.ID):
			helper = i
		case strings.Contains(command, "docker start"):
			start = i
		}
	}
	if !(stop >= 0 && stop < helper && helper < start) {
		t.Fatalf("expected stop < helper < start: %v", f.Commands)
	}
	command := f.Commands[helper]
	for _, want := range []string{"-v " + db.ID + "_data:/data", HelperImage, "--network none", "/backup:ro", "rm -rf /data/appendonlydir"} {
		if !strings.Contains(command, want) {
			t.Errorf("helper command lacks %q: %s", want, command)
		}
	}
}

func TestKeyValueRestoreNeedsTheTypedNameAndRefusesAnotherTarget(t *testing.T) {
	s, db, f := startKeyValue(t, EngineRedis)
	backup, _ := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
	done := waitBackup(t, s, backup.ID)
	if _, err := s.StartRestore(context.Background(), done.ID, RestoreInput{Confirmation: "wrong"}); err == nil {
		t.Error("the database name must be typed")
	}
	_, err := s.StartRestore(context.Background(), done.ID, RestoreInput{Confirmation: db.Name, NewName: "copy"})
	conflict(t, err)
	if f.Ran("docker stop") {
		t.Error("nothing may be stopped for a refused restore")
	}
}

func TestKeyValueRestoreStartsTheDatabaseAgainWhenTheCopyFails(t *testing.T) {
	s, db, f := startKeyValue(t, EngineKeyDB)
	backup, _ := s.StartBackup(context.Background(), db.ID, BackupTriggerManual)
	done := waitBackup(t, s, backup.ID)
	f.On("pier-restore-", "cp: no space left", 1)
	op, _ := s.StartRestore(context.Background(), done.ID, RestoreInput{Confirmation: db.Name})
	finished := waitKeyValueOperation(t, s, db.ID, op.ID)
	if finished.Status != "failed" || !f.Ran("docker start") {
		t.Fatalf("operation %+v, commands %v", finished, f.Commands)
	}
}

func waitKeyValueOperation(t *testing.T, s *Service, databaseID, id string) Operation {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ops, _ := s.ListOperations(context.Background(), databaseID)
		for _, op := range ops {
			if op.ID == id && op.Status != "running" {
				s.mu.Lock()
				busy := len(s.active)
				s.mu.Unlock()
				if busy == 0 {
					return op
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("operation did not finish")
	return Operation{}
}
