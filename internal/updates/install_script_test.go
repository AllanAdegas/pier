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
