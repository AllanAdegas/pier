package installtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// No daemon or network is contacted: Docker is a local fake executable.
func TestPrepareRootRefusesExistingData(t *testing.T) {
	root := filepath.Join(t.TempDir(), "pier-v08-installtest-existing")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "keep")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "-c", `. ./lib.sh; prepare_root`, "./test.sh")
	cmd.Env = append(os.Environ(), "INSTALLTEST_ROOT="+root, "PATH="+bin+":"+os.Getenv("PATH"))
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("accepted existing root: %s", out)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "preserve" {
		t.Fatalf("existing data altered: %q %v", data, err)
	}
}

func TestPrepareRootRejectsUnsafePathsBeforeDocker(t *testing.T) {
	for _, root := range []string{"relative-pier-v08-installtest", "/tmp/pier-v08-installtest/../other", "/tmp/pier-v08-installtest'quote", "/tmp/pier-v08-installtest\nline", "/tmp/unrelated"} {
		t.Run(strings.ReplaceAll(root, "/", "_"), func(t *testing.T) {
			cmd := exec.Command("sh", "-c", `. ./lib.sh; docker() { echo DOCKER_CALLED; }; prepare_root`, "./test.sh")
			cmd.Env = append(os.Environ(), "INSTALLTEST_ROOT="+root)
			out, err := cmd.CombinedOutput()
			if err == nil || strings.Contains(string(out), "DOCKER_CALLED") {
				t.Fatalf("unsafe root reached Docker: %s (%v)", out, err)
			}
		})
	}
}

func TestShellSyntax(t *testing.T) {
	files, err := filepath.Glob("*.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if out, err := exec.Command("sh", "-n", file).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s %v", file, out, err)
		}
	}
}

func TestVersionIsExactAndRequiresHTTPResponse(t *testing.T) {
	for _, body := range []string{`{"version":"0.7.0","api":"v1"}`, `{"version":"0x7x0"}`, ""} {
		cmd := exec.Command("sh", "-c", `. ./lib.sh; curl() { printf '%s' "$FAKE_BODY"; }; version_is 0.7.0`, "./test.sh")
		cmd.Env = append(os.Environ(), "FAKE_BODY="+body)
		out, err := cmd.CombinedOutput()
		if (err == nil) != strings.Contains(body, `"0.7.0"`) {
			t.Fatalf("body %q: %s %v", body, out, err)
		}
	}
	cmd := exec.Command("sh", "-c", `. ./lib.sh; curl() { echo '{"version":"0.7.0"}'; return 22; }; version_is 0.7.0`, "./test.sh")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("HTTP failure accepted: %s", out)
	}
}
