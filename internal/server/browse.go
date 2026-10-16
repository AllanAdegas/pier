package server

import (
	"context"
	"path"
	"sort"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/shell"
)

// maxListedEntries bounds a folder listing.
const maxListedEntries = 2000

// standardComposeFiles are the names docker compose looks for, in its order.
var standardComposeFiles = []string{"compose.yaml", "compose.yml", "docker-compose.yaml", "docker-compose.yml"}

// Listing is what a folder of a server contains that matters when choosing
// where a compose project lives: its subfolders, the compose files in it and
// whether it has a .env. File contents are never read.
type Listing struct {
	Path string `json:"path"`
	// Parent is empty at the root.
	Parent       string   `json:"parent"`
	Directories  []string `json:"directories"`
	ComposeFiles []string `json:"compose_files"`
	EnvFile      bool     `json:"env_file"`
	Truncated    bool     `json:"truncated"`
}

// Browse lists a folder of a server. An empty directory means the home of
// the user Pier connects as.
func (s *Service) Browse(ctx context.Context, id, directory string) (Listing, error) {
	ex, _, err := s.connector.Executor(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	if directory == "" {
		home, err := ex.Run(ctx, `printf %s "$HOME"`)
		if err != nil || !path.IsAbs(home) {
			home = "/"
		}
		directory = home
	}
	if !path.IsAbs(directory) || strings.ContainsAny(directory, "\x00\n\r") {
		return Listing{}, apperr.Invalid("Informe um caminho absoluto.")
	}
	directory = path.Clean(directory)
	// -L follows links, so a linked folder is listed as a folder; a broken
	// link makes ls complain, which must not hide the rest of the listing.
	command := shell.Join("test", "-d", directory) + " && " + shell.Join("test", "-r", directory) +
		" && { " + shell.Join("ls", "-1ApL", "--", directory) + " 2>/dev/null; true; }"
	output, err := ex.Run(ctx, command)
	if err != nil {
		return Listing{}, apperr.NotFound("Pasta")
	}
	return parseListing(directory, output), nil
}

// parseListing reads the output of `ls -1Ap`: one name per line, folders
// ending with a slash.
func parseListing(directory, output string) Listing {
	listing := Listing{Path: directory, Directories: []string{}, ComposeFiles: []string{}}
	if directory != "/" {
		listing.Parent = path.Dir(directory)
	}
	for index, line := range strings.Split(output, "\n") {
		if index >= maxListedEntries {
			listing.Truncated = true
			break
		}
		listing.add(strings.TrimRight(line, "\r"))
	}
	sort.Strings(listing.Directories)
	sort.SliceStable(listing.ComposeFiles, func(i, j int) bool {
		return composeRank(listing.ComposeFiles[i]) < composeRank(listing.ComposeFiles[j])
	})
	return listing
}

func (l *Listing) add(name string) {
	switch {
	case name == "":
	case strings.HasSuffix(name, "/"):
		l.Directories = append(l.Directories, strings.TrimSuffix(name, "/"))
	case name == ".env":
		l.EnvFile = true
	case isComposeFile(name):
		l.ComposeFiles = append(l.ComposeFiles, name)
	}
}

// isComposeFile recognizes compose files by the naming convention
// (compose.yaml, docker-compose.prod.yml…). Any other YAML in the folder is
// most likely something else, and offering it would only mislead.
func isComposeFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "compose") && (strings.HasSuffix(lower, ".yaml") || strings.HasSuffix(lower, ".yml"))
}

// composeRank puts the names docker compose looks for first, in its order.
func composeRank(name string) int {
	for index, standard := range standardComposeFiles {
		if name == standard {
			return index
		}
	}
	return len(standardComposeFiles)
}
