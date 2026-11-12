package application

import (
	"encoding/json"
	"path"
	"strings"
)

func validWatchPattern(pattern string) bool {
	if pattern == "" || len(pattern) > 512 || strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "\\") {
		return false
	}
	for _, part := range strings.Split(pattern, "/") {
		if part == ".." || part == "." || part == "" {
			return false
		}
		if part != "**" {
			if _, err := path.Match(part, ""); err != nil {
				return false
			}
		}
	}
	return true
}

func watchMatch(pattern, file string) bool {
	p, f := strings.Split(pattern, "/"), strings.Split(file, "/")
	seen := map[[2]int]bool{}
	var match func(int, int) bool
	match = func(i, j int) bool {
		state := [2]int{i, j}
		if seen[state] {
			return false
		}
		seen[state] = true
		if i == len(p) {
			return j == len(f)
		}
		if p[i] == "**" {
			return match(i+1, j) || (j < len(f) && match(i, j+1))
		}
		if j == len(f) {
			return false
		}
		ok, _ := path.Match(p[i], f[j])
		return ok && match(i+1, j+1)
	}
	return match(0, 0)
}

// Incomplete provider file lists fail open: skipping a deploy requires proof
// that no changed file matches, including deletions and renames.
func watchPathsChanged(patterns []string, body []byte) bool {
	if len(patterns) == 0 {
		return true
	}
	var event struct {
		Commits      []struct{ Added, Modified, Removed []string } `json:"commits"`
		TotalCommits int                                           `json:"total_commits"`
		Total        int                                           `json:"total_commits_count"`
		Size         int                                           `json:"size"`
		Truncated    bool                                          `json:"truncated"`
	}
	if json.Unmarshal(body, &event) != nil || len(event.Commits) == 0 || event.Total > len(event.Commits) || event.TotalCommits > len(event.Commits) || event.Size > len(event.Commits) || event.Truncated {
		return true
	}
	for _, commit := range event.Commits {
		if commit.Added == nil && commit.Modified == nil && commit.Removed == nil {
			return true
		}
		for _, files := range [][]string{commit.Added, commit.Modified, commit.Removed} {
			for _, file := range files {
				if len(file) > 4096 {
					return true
				}
				for _, pattern := range patterns {
					if watchMatch(pattern, file) {
						return true
					}
				}
			}
		}
	}
	return false
}
