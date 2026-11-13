package volumebackup

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
	"pier/internal/settings"
)

const (
	psLine      = "c1\tweb\timg\trunning\tUp 1 hour\tapplication\tapp1\t"
	mountsLine  = `[{"Type":"volume","Name":"app1_data","Source":"/var/lib/docker/volumes/app1_data/_data","Destination":"/data"},` +
		`{"Type":"bind","Source":"/data/pier/applications/app1/uploads","Destination":"/uploads"},` +
		`{"Type":"bind","Source":"/srv/shared","Destination":"/shared"},` +
		`{"Type":"bind","Source":"/var/run/docker.sock","Destination":"/var/run/docker.sock"}]`
	dataDir = "/data/pier"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1", Kind: server.KindSSH, Host: "h", DataDir: dataDir}, nil
}

type fakeResources struct {
	mu   sync.Mutex
	busy error
}

func (f *fakeResources) Resolve(_ context.Context, kind, id string) (Resource, error) {
	if id != "app1" {
		return Resource{}, apperr.NotFound("Aplicação")
	}
	return Resource{Kind: kind, ID: id, Name: "My App", ServerID: "srv1"}, nil
}

func (f *fakeResources) EnsureIdle(context.Context, string, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.busy
}

func (f *fakeResources) setBusy(err error) { f.mu.Lock(); f.busy = err; f.mu.Unlock() }

type recordedEvents struct {
	mu     sync.Mutex
	events []notify.Event
}

func (r *recordedEvents) Publish(event notify.Event) {
	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()
}

func (r *recordedEvents) all() []notify.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]notify.Event(nil), r.events...)
}

type fixture struct {
	service   *Service
	fake      *executor.Fake
	resources *fakeResources
	events    *recordedEvents
	box       *secretbox.Box
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	box, _ := secretbox.New("test-passphrase-123456")
	fake := executor.NewFake().
		On("docker ps", psLine, 0).
		On("docker inspect", mountsLine, 0).
		On("wc -c", "2048", 0)
	resources, events := &fakeResources{}, &recordedEvents{}
	service := NewService(db, box, fakeConnector{fake}, resources, events)
	return fixture{service: service, fake: fake, resources: resources, events: events, box: box}
}

func (f fixture) configureS3(t *testing.T) {
	t.Helper()
	store := settings.NewS3Store(f.service.store.db, f.box)
	err := store.Update(context.Background(), settings.S3Settings{Endpoint: "https://s3.example.com", Region: "us-east-1",
		Bucket: "bucket", Prefix: "pre", AccessKey: "AK", SecretKey: "SK"})
	if err != nil {
		t.Fatalf("configure s3: %v", err)
	}
}

func (f fixture) backupAndWait(t *testing.T) Backup {
	t.Helper()
	backup, err := f.service.Start(context.Background(), KindApplication, "app1", TriggerManual)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return waitFinished(t, f.service, backup.ID)
}

func waitFinished(t *testing.T, service *Service, id string) Backup {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		backup, err := service.store.backup(context.Background(), id)
		if err == nil && backup.Status != StatusRunning {
			waitReleased(service)
			return backup
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("backup %s did not finish", id)
	return Backup{}
}

// waitReleased waits for the background goroutine to give the resource back.
func waitReleased(service *Service) {
	for i := 0; i < 400; i++ {
		service.mu.Lock()
		idle := len(service.active) == 0
		service.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func commandsContaining(f *executor.Fake, fragment string) []string {
	var found []string
	for _, command := range f.Commands {
		if strings.Contains(command, fragment) {
			found = append(found, command)
		}
	}
	return found
}

func indexOfCommand(f *executor.Fake, fragment string) int {
	for i, command := range f.Commands {
		if strings.Contains(command, fragment) {
			return i
		}
	}
	return -1
}
