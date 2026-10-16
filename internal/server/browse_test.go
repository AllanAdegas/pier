package server

import (
	"strings"
	"testing"
)

func TestParseListingKeepsWhatMattersForChoosingAComposeFolder(t *testing.T) {
	output := strings.Join([]string{
		"src/", ".git/", "docker-compose.prod.yml", "README.md", ".env", ".env.production", "compose.yaml",
		"mkdocs.yml", "values.yaml", "compose-notes.txt",
		"docker-compose.yml", "secrets/", "id_rsa", "notes with spaces/",
	}, "\n")
	listing := parseListing("/home/deploy/shop", output)
	if listing.Parent != "/home/deploy" || !listing.EnvFile || listing.Truncated {
		t.Errorf("listing: %+v", listing)
	}
	if got := strings.Join(listing.Directories, "|"); got != ".git|notes with spaces|secrets|src" {
		t.Errorf("directories = %s", got)
	}
	// The names docker compose looks for come first, in its order; other
	// files (keys, documents, extra env files, unrelated YAML) are not listed.
	if got := strings.Join(listing.ComposeFiles, "|"); got != "compose.yaml|docker-compose.yml|docker-compose.prod.yml" {
		t.Errorf("compose files = %s", got)
	}
	if root := parseListing("/", ""); root.Parent != "" || len(root.Directories) != 0 {
		t.Errorf("the root has no parent: %+v", root)
	}
}
