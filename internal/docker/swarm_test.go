package docker

import (
	"context"
	"pier/internal/platform/executor"
	"strings"
	"testing"
)

func TestSwarmTokenUsesStdin(t *testing.T) {
	ex := executor.NewFake()
	token := "SWMTKN-1-secret-token"
	if err := SwarmJoin(context.Background(), ex, token, "10.0.0.1", ""); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range ex.Commands {
		if strings.Contains(cmd, token) {
			t.Fatal("token in command")
		}
		if string(ex.Inputs[cmd]) != token+"\n" {
			t.Fatal("missing stdin")
		}
	}
	ex.On("join-token", "prefix"+token, 0)
	if _, err := SwarmJoinToken(context.Background(), ex, SwarmWorker); err == nil {
		t.Fatal("invalid token accepted")
	}
	if strings.Contains(ScrubJoinTokens("failure "+token), token) {
		t.Fatal("unmasked token")
	}
}
func TestSwarmParsingAndValidation(t *testing.T) {
	ex := executor.NewFake().On("service ls", "id\tapi\t2/2\tnginx\tweb\nmalformed", 0)
	items, e := ListSwarmServices(context.Background(), ex, "test")
	if e != nil || len(items) != 1 || items[0].Process != "web" {
		t.Fatal(items, e)
	}
	if _, e = SwarmServiceLogsCommand("--bad", 20); e == nil {
		t.Fatal("option injection")
	}
	if e = ScaleSwarmService(context.Background(), ex, "api", -1); e == nil {
		t.Fatal("negative replicas")
	}
	ex.On("node ls", "abcdefghijklmnopqrstuvwxy\thost\tReady\tActive\tLeader\t27.0", 0)
	nodes, e := ListSwarmNodes(context.Background(), ex)
	if e != nil || len(nodes) != 1 || nodes[0].Status != "ready" {
		t.Fatal(nodes, e)
	}
}
