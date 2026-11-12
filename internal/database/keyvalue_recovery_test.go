package database

import (
	"context"
	"strings"
	"testing"
)

func TestKeyValueCrashRecoveryUsesDurableOriginalIDs(t *testing.T) {
	for _, engine := range []Engine{EngineRedis, EngineKeyDB, EngineDragonfly} {
		for _, point := range []string{"afterStop", "duringHelper", "beforeStart"} {
			t.Run(string(engine)+"/"+point, func(t *testing.T) {
				s, db, f := startKeyValue(t, engine)
				b := seedBackup(t, s, db, BackupSucceeded)
				_, err := s.store.db.Exec("INSERT INTO database_operations(id,database_id,backup_id,kind,status,started_at,resume_containers) VALUES('crash',?,?,'restore','running','2026-01-01',?)", db.ID, b.ID, `["original"]`)
				if err != nil {
					t.Fatal(err)
				}
				// The current inventory also contains an intentionally stopped replica and
				// a newly created replacement; neither was running before this operation.
				f.On("docker ps", "original\told\timg\texited\tExited\tdatabase\t"+db.ID+"\nstopped\treplica\timg\texited\tExited\tdatabase\t"+db.ID+"\nreplacement\tnew\timg\texited\tExited\tdatabase\t"+db.ID, 0)
				if point != "duringHelper" {
					f.On("docker rm -fv pier-restore-crash", "absent", 1).On("docker inspect pier-restore-crash", "absent", 1)
				}
				restarted := NewService(s.store, s.connector, discardEvents{})
				if err := restarted.RecoverBackups(context.Background()); err != nil {
					t.Fatal(err)
				}
				ops, _ := restarted.ListOperations(context.Background(), db.ID)
				if ops[0].Status != "failed" || !strings.Contains(ops[0].Message, "reinicialização") {
					t.Fatalf("%+v", ops)
				}
				if !f.Ran("docker start original") || f.Ran("docker start stopped") || f.Ran("docker start replacement") {
					t.Fatal(f.Commands)
				}
				cleanup, start := -1, -1
				for i, c := range f.Commands {
					if strings.Contains(c, "docker rm -fv") {
						cleanup = i
					}
					if strings.Contains(c, "docker start") {
						start = i
					}
				}
				if cleanup < 0 || start <= cleanup {
					t.Fatal(f.Commands)
				}
				f.Commands = nil
				if err := restarted.RecoverBackups(context.Background()); err != nil {
					t.Fatal(err)
				}
				if f.Ran("docker start") {
					t.Fatal("successful recovery must clear retry journal")
				}
			})
		}
	}
}

func TestKeyValueRecoveryRestartFailureIsReportedAndRetried(t *testing.T) {
	s, db, f := startKeyValue(t, EngineRedis)
	b := seedBackup(t, s, db, BackupSucceeded)
	_, err := s.store.db.Exec("INSERT INTO database_operations(id,database_id,backup_id,kind,status,started_at,resume_containers) VALUES('crash',?,?,'restore','running','2026-01-01',?)", db.ID, b.ID, `["original"]`)
	if err != nil {
		t.Fatal(err)
	}
	f.On("docker start", "restart failed", 1)
	restarted := NewService(s.store, s.connector, discardEvents{})
	if err := restarted.RecoverBackups(context.Background()); err != nil {
		t.Fatal(err)
	}
	ops, _ := restarted.ListOperations(context.Background(), db.ID)
	if ops[0].Status != "failed" || !strings.Contains(ops[0].Message, "iniciar o banco") || !strings.Contains(ops[0].Message, "reinicialização") {
		t.Fatalf("%+v", ops)
	}
	if err := restarted.ensureIdle(context.Background(), db.ID); err == nil {
		t.Fatal("pending recovery must protect resource")
	}
	f.Commands = nil
	f.On("docker start", "", 0)
	if err := restarted.RecoverBackups(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !f.Ran("docker start original") {
		t.Fatal("failed restart was not retried")
	}
	if err := restarted.ensureIdle(context.Background(), db.ID); err != nil {
		t.Fatal(err)
	}
}

func TestKeyValueRestoreJournalsBeforeStop(t *testing.T) {
	s, db, f := startKeyValue(t, EngineRedis)
	b := seedBackup(t, s, db, BackupSucceeded)
	_, err := s.store.db.Exec("INSERT INTO database_operations(id,database_id,backup_id,kind,status,started_at) VALUES('restore',?,?,'restore','running','2026-01-01')", db.ID, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	checked := false
	err = s.restoreKeyValue(context.Background(), db, db, b, Operation{ID: "restore", Kind: "restore"}, func(string) {
		if checked {
			return
		}
		checked = true
		var journal string
		if err := s.store.db.QueryRow("SELECT resume_containers FROM database_operations WHERE id='restore'").Scan(&journal); err != nil {
			t.Fatal(err)
		}
		if journal != `["container"]` || f.Ran("docker stop") {
			t.Fatalf("journal=%s commands=%v", journal, f.Commands)
		}
	})
	if err != nil || !checked {
		t.Fatalf("checked=%v err=%v", checked, err)
	}
	// A missing operation must fail closed before any stop.
	f.Commands = nil
	err = s.restoreKeyValue(context.Background(), db, db, b, Operation{ID: "missing", Kind: "restore"}, func(string) {})
	if err == nil || f.Ran("docker stop") {
		t.Fatal("stop without durable journal")
	}
}
