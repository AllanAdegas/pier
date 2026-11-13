package updates

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const repositoryRoot = "../.."

func TestInstallScriptIsValidPOSIXShell(t *testing.T) {
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh available")
	}
	output, err := exec.Command(shell, "-n", filepath.Join(repositoryRoot, "install.sh")).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n install.sh: %v\n%s", err, output)
	}
}

func TestInstallPullsThePublishedImageUnlessBuildIsRequested(t *testing.T) {
	script := readRepositoryFile(t, "install.sh")
	for _, want := range []string{`PIER_BUILD:-0`, "pull pier", "--no-build", "internal/version/version.go"} {
		if !strings.Contains(script, want) {
			t.Errorf("install.sh lacks %q", want)
		}
	}
	compose := readRepositoryFile(t, "compose.yaml")
	if !strings.Contains(compose, "image: ghcr.io/allanadegas/pier:${PIER_VERSION:-latest}") {
		t.Error("compose.yaml does not reference the published image through PIER_VERSION")
	}
}

func readRepositoryFile(t *testing.T, name string) string {
	t.Helper()
	content, err := os.ReadFile(filepath.Join(repositoryRoot, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func TestInstallGeneratedDataDirectorySurvivesShellLoading(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"install.sh", ".env.example", "internal/version/version.go"} {
		content, err := os.ReadFile(filepath.Join(repositoryRoot, name))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "docker"), []byte("#!/bin/sh\ncase \"$*\" in *auth/status*) printf '%s' '{\"needs_setup\":false}';; esac\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data with spaces & | $literal `literal`")
	cmd := exec.Command("sh", filepath.Join(root, "install.sh"))
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "PIER_DATA_DIR="+data)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("install failed: %v\n%s", err, out)
	}
	cmd = exec.Command("sh", "-c", `. "$1"; printf '%s' "$PIER_DATA_DIR"`, "sh", filepath.Join(root, ".env"))
	out, err := cmd.CombinedOutput()
	if err != nil || string(out) != data {
		t.Fatalf("env roundtrip: %q %v; want %q", out, err, data)
	}
	if info, err := os.Stat(data); err != nil || !info.IsDir() {
		t.Fatalf("data directory not created: %v", err)
	}
}
