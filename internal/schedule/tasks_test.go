package schedule

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/application"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type taskApps struct{}

func (taskApps) Get(context.Context, string) (application.Application, error) {
	return application.Application{ID: "app1", ServerID: "srv1"}, nil
}
func (taskApps) OutputSecrets(context.Context, string) ([]string, error) {
	return []string{"  dummy-current-secret  ", "line-one-secret\nline-two-secret"}, nil
}

type taskConnector struct{ ex executor.Executor }

func (c taskConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return c.ex, server.Server{ID: "srv1"}, nil
}

type taskEvents struct{ events []notify.Event }

func (p *taskEvents) Publish(event notify.Event) { p.events = append(p.events, event) }
func taskFixture(t *testing.T) (*TaskService, *executor.Fake, *taskEvents, Task) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "tasks.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	seedTaskPlacement(t, db)
	ex := executor.NewFake()
	ex.On("docker ps", "container\tapp\timage\trunning\tUp\tapplication\tapp1\tdep1", 0)
	ex.On("{{json .Config.Env}}", `["TOKEN=dummy-previous-secret"]`, 0)
	events := &taskEvents{}
	service := NewTaskService(db, taskApps{}, taskConnector{ex}, events)
	task, err := service.Create(context.Background(), "app1", TaskInput{Name: "command", Command: "print-env", Cron: "* * * * *", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return service, ex, events, task
}
func seedTaskPlacement(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, statement := range []string{
		`INSERT INTO servers (id,name,kind,data_dir,created_at) VALUES ('srv1','test','local','/data','2026-01-01T00:00:00Z')`,
		`INSERT INTO projects (id,name,created_at) VALUES ('p','p','2026-01-01T00:00:00Z')`,
		`INSERT INTO environments(id,project_id,name,created_at) VALUES ('e','p','e','2026-01-01T00:00:00Z')`,
		`INSERT INTO applications(id,environment_id,server_id,name,config,webhook_secret,created_at,updated_at) VALUES ('app1','e','srv1','app','{}','sealed-test-secret','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
}
func TestTaskOutputIsMaskedBeforePersistenceAndNotification(t *testing.T) {
	service, ex, events, task := taskFixture(t)
	ex.On("docker exec container sh -c", "dummy-current-secret\nline-two-secret\ndummy-previous-secret", 1)
	execution, err := service.Run(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := service.db.QueryRow(`SELECT output FROM task_executions WHERE id=?`, execution.ID).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if execution.Status != "failed" || len(events.events) != 1 {
		t.Fatalf("%s events %v", execution.Status, events.events)
	}
	all := stored + execution.Output + events.events[0].Message
	for _, secret := range []string{"dummy-current-secret", "line-two-secret", "dummy-previous-secret"} {
		if strings.Contains(all, secret) {
			t.Fatal("leaked", secret)
		}
	}
	if !strings.Contains(stored, "••••••") {
		t.Fatal(stored)
	}
	// Legacy persisted output is masked on read; raw access is a separate handler guard.
	_, err = service.db.Exec(`UPDATE task_executions SET output=?, output_masked=0 WHERE id=?`, "dummy-rotated-legacy-secret", execution.ID)
	if err != nil {
		t.Fatal(err)
	}
	masked, err := service.Executions(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := service.Executions(context.Background(), task.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if masked[0].Output != "••••••" || raw[0].Output != "dummy-rotated-legacy-secret" {
		t.Fatalf("masked=%q raw=%q", masked[0].Output, raw[0].Output)
	}
}

type blockingTaskExecutor struct {
	*executor.Fake
	started, release chan struct{}
	once             sync.Once
}

func (e *blockingTaskExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if strings.Contains(command, "docker exec container sh -c") {
		e.once.Do(func() { close(e.started) })
		<-e.release
	}
	return e.Fake.Stream(ctx, command, sink)
}
func TestTaskManualAndCronShareSingleFlightGuard(t *testing.T) {
	service, fake, _, task := taskFixture(t)
	ex := &blockingTaskExecutor{Fake: fake, started: make(chan struct{}), release: make(chan struct{})}
	service.connector = taskConnector{ex}
	done := make(chan error, 1)
	go func() { _, err := service.Run(context.Background(), task.ID); done <- err }()
	select {
	case <-ex.started:
	case <-time.After(time.Second):
		t.Fatal("task did not start")
	}
	defer func() {
		close(ex.release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	_, err := service.Run(context.Background(), task.ID)
	appErr, ok := apperr.As(err)
	if !ok || appErr.Kind != apperr.KindConflict {
		t.Fatalf("overlap %v", err)
	}
	jobs, err := service.Jobs(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	jobs[0].Run()
	var count int
	if err := service.db.QueryRow(`SELECT COUNT(*) FROM task_executions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("overlapping task created %d runs", count)
	}
}
func TestTaskOutputBoundAppliesAfterMasking(t *testing.T) {
	service, ex, _, task := taskFixture(t)
	ex.On("docker exec container sh -c", strings.Repeat("x", maxTaskOutputBytes-2)+"dummy-current-secret", 0)
	execution, err := service.Run(context.Background(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(execution.Output) > maxTaskOutputBytes || strings.Contains(execution.Output, "dummy-") {
		t.Fatal("output leaked or exceeded cap")
	}
}
func TestSchedulerSkipsConcurrentRuns(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	s := NewScheduler(slog.New(slog.NewTextHandler(io.Discard, nil)), func(context.Context) ([]Job, error) {
		return []Job{{Name: "test", Cron: "* * * * *", Run: func() { entered <- struct{}{}; <-release }}}, nil
	})
	s.Reload(context.Background())
	job := s.engine.Entries()[0].WrappedJob
	done := make(chan struct{})
	go func() { job.Run(); close(done) }()
	<-entered
	job.Run()
	close(release)
	<-done
	select {
	case <-entered:
		t.Fatal("concurrent cron executed")
	default:
	}
}
