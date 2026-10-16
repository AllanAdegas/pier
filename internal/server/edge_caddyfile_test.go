package server

import (
	"strings"
	"testing"
)

// The Caddyfile found on the test VPS, owned by another project.
const foreignCaddyfile = `{$APP_DOMAIN} {
	reverse_proxy frontend:80
}

{$LIVEKIT_DOMAIN} {
	reverse_proxy livekit-server:7880
}
`

func TestEdgeAddressesFollowTheSchemesOfTheRoutes(t *testing.T) {
	got := edgeAddresses([]string{
		"https://n8n.example.com", "http://n8n.1.2.3.4.sslip.io",
		"https://both.example.com", "http://both.example.com", "https://n8n.example.com", "garbage",
	})
	want := []string{"http://both.example.com", "https://both.example.com", "http://n8n.1.2.3.4.sslip.io", "n8n.example.com"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("addresses = %v, want %v", got, want)
	}
}

func TestTheEdgeBlockLeavesTheRestOfTheCaddyfileAlone(t *testing.T) {
	block := renderEdgeBlock([]string{"n8n.example.com"}, "172.18.0.1:8880")
	first, err := replaceEdgeBlock(foreignCaddyfile, block)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(first, foreignCaddyfile) || !strings.Contains(first, "n8n.example.com {\n\treverse_proxy 172.18.0.1:8880\n}") {
		t.Fatalf("block not appended after the existing sites:\n%s", first)
	}
	again, _ := replaceEdgeBlock(first, block)
	if again != first {
		t.Errorf("replacing with the same block must not change the file:\n%s", again)
	}
	other := renderEdgeBlock([]string{"other.example.com"}, "172.18.0.1:8880")
	replaced, _ := replaceEdgeBlock(first, other)
	if strings.Contains(replaced, "n8n.example.com") || strings.Count(replaced, edgeBlockStart) != 1 {
		t.Errorf("the old block must be replaced, not kept:\n%s", replaced)
	}
	removed, _ := replaceEdgeBlock(replaced, "")
	if removed != foreignCaddyfile {
		t.Errorf("without domains the file must return to the original:\n%q", removed)
	}
}

func TestABrokenPierBlockIsNotTouched(t *testing.T) {
	broken := foreignCaddyfile + edgeBlockStart + "\nx.example.com {\n}\n"
	if _, err := replaceEdgeBlock(broken, ""); err == nil {
		t.Error("a block without its end marker must be refused, not guessed")
	}
}
