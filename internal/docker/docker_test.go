package docker

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"pier/internal/platform/executor"
)

func TestListManagedKeepsContainersWithEmptyTrailingLabels(t *testing.T) {
	fake := executor.NewFake()
	// The proxy has no deployment label, so docker prints a trailing tab that
	// is trimmed away with the surrounding whitespace.
	fake.On("docker ps", "abc\tpier-proxy\ttraefik:v3.6\trunning\tUp 4 seconds\tproxy\tproxy\t", 0)

	containers, err := ListManaged(context.Background(), fake)
	if err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	if len(containers) != 1 {
		t.Fatalf("got %d containers, want 1", len(containers))
	}
	got := containers[0]
	if got.Type != TypeProxy || got.ResourceID != "proxy" || got.Deployment != "" {
		t.Errorf("unexpected container: %+v", got)
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		name       string
		containers []Container
		want       string
	}{
		{"no containers", nil, StatusStopped},
		{"all exited", []Container{{State: "exited"}}, StatusStopped},
		{"running without healthcheck", []Container{{State: "running", Status: "Up 2 minutes"}}, StatusRunning},
		{"healthy", []Container{{State: "running", Status: "Up 2 minutes (healthy)"}}, StatusRunning},
		{"unhealthy", []Container{{State: "running", Status: "Up 2 minutes (unhealthy)"}}, StatusUnhealthy},
		{"starting", []Container{{State: "running", Status: "Up 2 seconds (health: starting)"}}, StatusStarting},
		{"crash loop", []Container{{State: "restarting"}}, StatusRestarting},
		{"partially down", []Container{{State: "running", Status: "Up"}, {State: "exited"}}, StatusDegraded},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Summarize(tc.containers); got != tc.want {
				t.Errorf("Summarize() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Only some docker commands expand "\t" in --format (ps does, info does not),
// so templates must carry real tabs.
func TestFormatColumnsUsesRealTabs(t *testing.T) {
	format := FormatColumns("{{.A}}", "{{.B}}")
	if format != "{{.A}}\t{{.B}}" || strings.Contains(format, `\t`) {
		t.Fatalf("columns must be separated by a real tab, got %q", format)
	}
	fake := executor.NewFake()
	if _, err := ListManaged(context.Background(), fake); err != nil {
		t.Fatalf("ListManaged: %v", err)
	}
	if command := fake.Commands[0]; strings.Contains(command, `\t`) || !strings.Contains(command, "\t") {
		t.Errorf("docker ps must receive real tabs, got %q", command)
	}
}

func TestExplainTellsWhatToDoWhenNetworksRunOut(t *testing.T) {
	raw := errors.New("docker compose up: command exited with status 1: failed to create network x_default: " +
		"Error response from daemon: all predefined address pools have been fully subnetted")
	explained := Explain(raw)
	if !strings.Contains(explained.Error(), "default-address-pools") || !errors.Is(explained, raw) {
		t.Errorf("the remedy must be named and the original error kept: %v", explained)
	}
	other := errors.New("pull access denied")
	if Explain(other) != other || Explain(nil) != nil {
		t.Error("other errors pass through untouched")
	}
}

func TestReadableNamesAreValidDockerNames(t *testing.T) {
	valid := regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$`)
	cases := map[string]string{
		"Blog API":                           "blog-api-k3j2xq",
		"  Postgres — Clientes (Produção)  ": "postgres-clientes-producao-k3j2xq",
		"n8n":                                "n8n-k3j2xq",
		"!!!":                                "pier-k3j2xq",
		"Um nome muito comprido que passa do limite de trinta": "um-nome-muito-comprido-que-pas-k3j2xq",
		"a__b..c": "a-b-c-k3j2xq",
	}
	for name, want := range cases {
		got := ReadableName(name, "k3j2xq8wz1ab4c")
		if got != want {
			t.Errorf("ReadableName(%q) = %q, want %q", name, got, want)
		}
		if !valid.MatchString(got) {
			t.Errorf("%q is not a valid lowercase Docker name", got)
		}
	}
	if ShortID("abc") != "abc" {
		t.Error("a short ID stays whole")
	}
}
