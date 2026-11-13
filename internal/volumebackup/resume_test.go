package volumebackup

import (
	"context"
	"strings"
	"testing"
)

func TestBackupResumeFailureIsNotSuccess(t *testing.T) {
	for _, archiveFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume_only", true: "both_fail"}[archiveFailure], func(t *testing.T) {
			f := newFixture(t)
			_, err := f.service.SetPolicy(context.Background(), KindApplication, "app1", Policy{Quiesce: true})
			if err != nil {
				t.Fatal(err)
			}
			f.fake.On("docker start", "resume-failed", 1)
			if archiveFailure {
				f.fake.On("tar -czf", "archive-failed", 1)
			}
			b := f.backupAndWait(t)
			if b.Status != StatusFailed || !strings.Contains(b.Message, "resume-failed") {
				t.Fatalf("resume failure hidden: %+v", b)
			}
			if archiveFailure && !strings.Contains(b.Message, "archive-failed") {
				t.Fatalf("primary error lost: %+v", b)
			}
		})
	}
}

func TestRestoreResumeFailureIsNotSuccess(t *testing.T) {
	for _, extractFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume_only", true: "both_fail"}[extractFailure], func(t *testing.T) {
			f := newFixture(t)
			b := f.backupAndWait(t)
			f.fake.On("docker start", "resume-failed", 1)
			if extractFailure {
				f.fake.On("tar -xzf", "extract-failed", 1)
			}
			op, err := f.service.StartRestore(context.Background(), b.ID, RestoreInput{Confirmation: "My App"})
			if err != nil {
				t.Fatal(err)
			}
			waitReleased(f.service)
			ops, err := f.service.Operations(context.Background(), KindApplication, "app1")
			if err != nil {
				t.Fatal(err)
			}
			for _, got := range ops {
				if got.ID == op.ID {
					if got.Status != StatusFailed || !strings.Contains(got.Message, "resume-failed") {
						t.Fatalf("resume failure hidden: %+v", got)
					}
					if extractFailure && !strings.Contains(got.Message, "extract-failed") {
						t.Fatalf("primary error lost: %+v", got)
					}
					return
				}
			}
			t.Fatal("restore operation missing")
		})
	}
}
