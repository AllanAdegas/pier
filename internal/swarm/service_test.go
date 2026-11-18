package swarm

import (
	"context"
	"path/filepath"
	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
	"testing"
	"time"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{}, nil
}
func fixture(t *testing.T) (*Service, *executor.Fake) {
	t.Helper()
	db, e := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	_, e = db.Exec("INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('local','local','local','/tmp/pier',?)", sqlite.FormatTime(time.Now()))
	if e != nil {
		t.Fatal(e)
	}
	var serverID string
	if e = db.QueryRow("SELECT id FROM servers LIMIT 1").Scan(&serverID); e != nil {
		t.Fatal(e)
	}
	ex := executor.NewFake().On("info", "active\tabcdefghijklmnopqrstuvwxy\ttrue", 0)
	s := NewService(db, fakeConnector{ex})
	e = s.store.insertCluster(context.Background(), Cluster{ID: "test", Name: "test", AdvertiseAddr: "10.0.0.1", CreatedAt: time.Now()}, Node{ServerID: serverID, NodeID: "abcdefghijklmnopqrstuvwxy", Role: "manager", Primary: true, JoinedAt: time.Now()})
	if e != nil {
		t.Fatal(e)
	}
	return s, ex
}
func TestServiceOwnershipAndLifecycle(t *testing.T) {
	s, ex := fixture(t)
	ctx := context.Background()
	if e := s.Action(ctx, "test", "foreign", "remove", 0); e == nil || ex.Ran("service rm") {
		t.Fatal("foreign service accepted", e)
	}
	ex.On("service ls", "id\tpier-test-api\t1/1\tnginx:stable\tweb", 0)
	for _, action := range []string{"scale", "restart", "rollback", "remove"} {
		if e := s.Action(ctx, "test", "pier-test-api", action, 2); e != nil {
			t.Fatal(action, e)
		}
	}
	if e := s.Action(ctx, "test", "pier-test-api", "scale", 101); e == nil {
		t.Fatal("unbounded replicas")
	}
	if e := s.Deploy(ctx, "test", DeployInput{Name: "api", Image: "nginx:stable", Replicas: 2}); e != nil {
		t.Fatal(e)
	}
	if e := s.Deploy(ctx, "test", DeployInput{Name: "new", Image: "nginx:stable", Replicas: 1}); e != nil {
		t.Fatal(e)
	}
	if !ex.Ran("service create") || !ex.Ran("pier.id=test") {
		t.Fatal(ex.Commands)
	}
	if _, e := s.Logs(ctx, "test", "pier-test-api"); e != nil {
		t.Fatal(e)
	}
	if _, e := s.Tasks(ctx, "test", "pier-test-api"); e != nil {
		t.Fatal(e)
	}
}
func TestDestructiveGuards(t *testing.T) {
	s, ex := fixture(t)
	ctx := context.Background()
	if e := s.Delete(ctx, "test", "wrong"); e == nil {
		t.Fatal("confirmation bypass")
	}
	ex.On("service ls", "unmanaged", 0)
	if e := s.Delete(ctx, "test", "test"); e == nil || ex.Ran("swarm leave") {
		t.Fatal("deleted occupied swarm", e)
	}
	if e := s.RemoveNode(ctx, "test", "local", "test"); e == nil {
		t.Fatal("removed manager")
	}
}
func TestValidation(t *testing.T) {
	s, ex := fixture(t)
	for _, i := range []DeployInput{{Name: "--bad", Image: "nginx", Replicas: 1}, {Name: "ok", Image: "bad; command", Replicas: 1}, {Name: "ok", Image: "nginx", Replicas: -1}} {
		if e := s.Deploy(context.Background(), "test", i); e == nil {
			t.Fatal(i)
		}
	}
	if len(ex.Commands) != 0 {
		t.Fatal("invalid input executed", ex.Commands)
	}
	if validAddress("0.0.0.0") || validAddress("host;cmd") || !validAddress("10.1.1.1") {
		t.Fatal("address validation")
	}
	if e := docker.SetNodeAvailability(context.Background(), ex, "--bad", "active"); e == nil {
		t.Fatal("invalid node")
	}
}
