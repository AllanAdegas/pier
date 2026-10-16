package shell

import (
	"os/exec"
	"testing"
)

// Every hostile value must reach the program as one unchanged argument.
func TestQuoteSurvivesARealShell(t *testing.T) {
	values := []string{
		"", "simple", "with space", "it's", `"double"`, "$(touch /tmp/pier-pwned)", "`id`", "a;b|c&d",
		"new\nline", "back\\slash", "*", "~", "--flag=value", "'; rm -rf / #",
	}
	for _, value := range values {
		output, err := exec.Command("sh", "-c", "printf %s "+Quote(value)).Output()
		if err != nil {
			t.Errorf("shell rejected Quote(%q): %v", value, err)
			continue
		}
		if string(output) != value {
			t.Errorf("Quote(%q) round-tripped as %q", value, output)
		}
	}
}

func TestJoinQuotesEachArgument(t *testing.T) {
	if got, want := Join("docker", "run", "a b", "x=y"), "docker run 'a b' x=y"; got != want {
		t.Errorf("Join = %q, want %q", got, want)
	}
}
