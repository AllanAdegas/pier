package server

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

var cleanupID = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)

type CleanupObject struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}
type CleanupReport struct {
	DryRun     bool            `json:"dry_run"`
	Candidates []CleanupObject `json:"candidates"`
	Removed    []CleanupObject `json:"removed"`
}
type cleanupInspect struct {
	ID       string `json:"Id"`
	Created  string
	RepoTags []string
	Config   struct{ Labels map[string]string }
	State    struct{ Status, FinishedAt string }
}

func cleanupEligible(ctx context.Context, ex executor.Executor, before time.Time, dry bool) (CleanupReport, error) {
	report := CleanupReport{DryRun: dry, Candidates: []CleanupObject{}, Removed: []CleanupObject{}}
	for _, kind := range []string{"container", "image"} {
		args := []string{"docker", kind, "ls", "-aq", "--filter", "label=pier.managed=true", "--filter", "label=pier.cleanup=true"}
		if kind == "container" {
			args = append(args, "--filter", "status=exited")
		} else {
			args = append(args, "--filter", "dangling=true")
		}
		ids, err := ex.Run(ctx, shell.Join(args...))
		if err != nil {
			return report, err
		}
		seen := map[string]bool{}
		for _, id := range strings.Fields(ids) {
			if !cleanupID.MatchString(id) || seen[id] {
				continue
			}
			seen[id] = true
			raw, err := ex.Run(ctx, shell.Join("docker", kind, "inspect", id))
			if err != nil {
				return report, err
			}
			var objs []cleanupInspect
			if err := json.Unmarshal([]byte(raw), &objs); err != nil {
				return report, err
			}
			if len(objs) != 1 {
				continue
			}
			obj := objs[0]
			labels := obj.Config.Labels
			if labels["pier.managed"] != "true" || labels["pier.cleanup"] != "true" {
				continue
			}
			// Only explicitly disposable scratch objects; never apps, DBs, previews,
			// stacks, infrastructure or backup helpers, even if incorrectly opted in.
			if labels["pier.type"] != "scratch" {
				continue
			}
			stamp := obj.Created
			if kind == "container" {
				if obj.State.Status != "exited" {
					continue
				}
				stamp = obj.State.FinishedAt
			} else {
				tagged := false
				for _, tag := range obj.RepoTags {
					if tag != "<none>:<none>" {
						tagged = true
					}
				}
				if tagged {
					continue
				}
			}
			at, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil || at.IsZero() || at.After(before) {
				continue
			}
			item := CleanupObject{Kind: kind, ID: id}
			report.Candidates = append(report.Candidates, item)
			if !dry {
				if _, err := ex.Run(ctx, shell.Join("docker", kind, "rm", id)); err != nil {
					return report, err
				}
				report.Removed = append(report.Removed, item)
			}
		}
	}
	return report, nil
}
