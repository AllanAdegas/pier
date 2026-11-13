package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// The safe cleanup removes only what Pier created and can recreate:
//
//   - stopped containers labeled pier.managed=true, stopped for longer than
//     the minimum age, whose resource no longer exists in Pier (orphans of a
//     failed removal, one-off helpers). Containers of registered resources are
//     kept: "Iniciar" restarts those same containers. The proxy is never
//     removed. Volumes are never removed (docker rm without -v);
//   - dangling images labeled pier.managed=true (old builds of Pier), older
//     than the minimum age, removed without --force so an image still used
//     by a container stays;
//   - the build cache of Pier's own Buildx builders (pier-build-*), older than
//     the minimum age. The default builder and other builders are not Pier's.
//
// A dry run lists the same objects and changes nothing.

const (
	CleanupContainer  = "container"
	CleanupImage      = "image"
	CleanupBuildCache = "build_cache"
	inspectBatch      = 50
)

var (
	cleanupID      = regexp.MustCompile(`^(sha256:)?[a-f0-9]{12,64}$`)
	pierBuilder    = regexp.MustCompile(`^pier-build-[0-9a-f]{16}$`)
	stoppedFilters = []string{"--filter", "status=exited", "--filter", "status=dead", "--filter", "status=created"}
)

// CleanupObject is one Docker object the cleanup found.
type CleanupObject struct {
	Kind   string `json:"kind"`
	ID     string `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// CleanupNote is an object left in place, with the reason.
type CleanupNote struct {
	CleanupObject
	Reason string `json:"reason"`
}

// CleanupReport tells what the cleanup removed (or would remove).
type CleanupReport struct {
	DryRun      bool            `json:"dry_run"`
	MinAgeHours int             `json:"min_age_hours"`
	Candidates  []CleanupObject `json:"candidates"`
	Removed     []CleanupObject `json:"removed"`
	Kept        []CleanupNote   `json:"kept"`
	Failed      []CleanupNote   `json:"failed"`
	FinishedAt  time.Time       `json:"finished_at"`
}

type cleanupInspect struct {
	ID       string `json:"Id"`
	Name     string
	Created  string
	RepoTags []string
	Config   struct{ Labels map[string]string }
	State    struct{ Status, FinishedAt string }
}

// ownerLookup tells whether the resource a container belongs to still exists.
type ownerLookup interface {
	exists(ctx context.Context, resourceType, id string) (bool, error)
}

type cleanupSweep struct {
	ex       executor.Executor
	owners   ownerLookup
	before   time.Time
	ageHours int
	dryRun   bool
	report   CleanupReport
}

func (s *cleanupSweep) run(ctx context.Context) (CleanupReport, error) {
	s.report = CleanupReport{DryRun: s.dryRun, MinAgeHours: s.ageHours, Candidates: []CleanupObject{},
		Removed: []CleanupObject{}, Kept: []CleanupNote{}, Failed: []CleanupNote{}}
	if err := s.containers(ctx); err != nil {
		return s.report, err
	}
	if err := s.images(ctx); err != nil {
		return s.report, err
	}
	s.buildCache(ctx)
	return s.report, nil
}

func (s *cleanupSweep) containers(ctx context.Context) error {
	args := append([]string{"docker", "container", "ls", "-a", "--no-trunc", "-q", "--filter", docker.LabelManaged + "=true"}, stoppedFilters...)
	objects, err := s.inspect(ctx, CleanupContainer, args)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if err := s.considerContainer(ctx, object); err != nil {
			return err
		}
	}
	return nil
}

func (s *cleanupSweep) considerContainer(ctx context.Context, object cleanupInspect) error {
	labels := object.Config.Labels
	if labels[docker.LabelManaged] != "true" || !stoppedState(object.State.Status) {
		return nil
	}
	stamp := object.State.FinishedAt
	if parsed, err := time.Parse(time.RFC3339Nano, stamp); err != nil || parsed.Year() < 2000 {
		stamp = object.Created
	}
	if !s.oldEnough(stamp) {
		return nil
	}
	item := CleanupObject{Kind: CleanupContainer, ID: object.ID, Name: strings.TrimPrefix(object.Name, "/"),
		Detail: labels[docker.LabelType] + " " + labels[docker.LabelID]}
	if labels[docker.LabelType] == docker.TypeProxy {
		s.keep(item, i18n.Instance().Text("Proxy do servidor."))
		return nil
	}
	owned, err := s.owners.exists(ctx, labels[docker.LabelType], labels[docker.LabelID])
	if err != nil {
		return err
	}
	if owned {
		s.keep(item, i18n.Instance().Text("Pertence a um recurso cadastrado. Remova-o pela página do recurso."))
		return nil
	}
	s.remove(ctx, item, shell.Join("docker", "container", "rm", object.ID))
	return nil
}

func (s *cleanupSweep) images(ctx context.Context) error {
	args := []string{"docker", "image", "ls", "--no-trunc", "-q", "--filter", "dangling=true", "--filter", "label=" + docker.LabelManaged + "=true"}
	objects, err := s.inspect(ctx, CleanupImage, args)
	if err != nil {
		return err
	}
	for _, object := range objects {
		if object.Config.Labels[docker.LabelManaged] != "true" || tagged(object.RepoTags) || !s.oldEnough(object.Created) {
			continue
		}
		item := CleanupObject{Kind: CleanupImage, ID: object.ID, Name: docker.ShortID(strings.TrimPrefix(object.ID, "sha256:")),
			Detail: object.Config.Labels[docker.LabelID]}
		s.remove(ctx, item, shell.Join("docker", "image", "rm", object.ID))
	}
	return nil
}

func (s *cleanupSweep) buildCache(ctx context.Context) {
	output, err := s.ex.Run(ctx, shell.Join("docker", "buildx", "ls", "--format", "{{.Name}}"))
	if err != nil {
		s.report.Kept = append(s.report.Kept, CleanupNote{CleanupObject: CleanupObject{Kind: CleanupBuildCache, ID: "buildx"},
			Reason: i18n.Instance().Text("Docker Buildx indisponível: o cache de build não foi verificado.")})
		return
	}
	seen := map[string]bool{}
	until := fmt.Sprintf("until=%dh", s.ageHours)
	for _, name := range strings.Fields(output) {
		if !pierBuilder.MatchString(name) || seen[name] {
			continue
		}
		seen[name] = true
		item := CleanupObject{Kind: CleanupBuildCache, ID: name, Name: name}
		if s.dryRun {
			usage, err := s.ex.Run(ctx, shell.Join("docker", "buildx", "du", "--builder", name, "--filter", until))
			item.Detail = summaryLine(usage, err, "Reclaimable:")
			s.report.Candidates = append(s.report.Candidates, item)
			continue
		}
		s.report.Candidates = append(s.report.Candidates, item)
		pruned, err := s.ex.Run(ctx, shell.Join("docker", "buildx", "prune", "--builder", name, "--force", "--filter", until))
		if err != nil {
			s.fail(item, err)
			continue
		}
		item.Detail = summaryLine(pruned, nil, "Total:")
		s.report.Removed = append(s.report.Removed, item)
	}
}

// inspect lists object ids with a docker command and inspects them in batches.
func (s *cleanupSweep) inspect(ctx context.Context, kind string, list []string) ([]cleanupInspect, error) {
	output, err := s.ex.Run(ctx, shell.Join(list...))
	if err != nil {
		return nil, err
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, id := range strings.Fields(output) {
		if cleanupID.MatchString(id) && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	objects := []cleanupInspect{}
	for start := 0; start < len(ids); start += inspectBatch {
		batch := ids[start:min(start+inspectBatch, len(ids))]
		raw, err := s.ex.Run(ctx, shell.Join(append([]string{"docker", kind, "inspect"}, batch...)...))
		if err != nil {
			return nil, err
		}
		var parsed []cleanupInspect
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return nil, fmt.Errorf("docker %s inspect: %w", kind, err)
		}
		for _, object := range parsed {
			if seen[object.ID] {
				objects = append(objects, object)
			}
		}
	}
	return objects, nil
}

func (s *cleanupSweep) remove(ctx context.Context, item CleanupObject, command string) {
	s.report.Candidates = append(s.report.Candidates, item)
	if s.dryRun {
		return
	}
	if _, err := s.ex.Run(ctx, command); err != nil {
		s.fail(item, err)
		return
	}
	s.report.Removed = append(s.report.Removed, item)
}

func (s *cleanupSweep) keep(item CleanupObject, reason string) {
	s.report.Kept = append(s.report.Kept, CleanupNote{CleanupObject: item, Reason: reason})
}

func (s *cleanupSweep) fail(item CleanupObject, err error) {
	s.report.Failed = append(s.report.Failed, CleanupNote{CleanupObject: item, Reason: lastLine(err.Error())})
}

func (s *cleanupSweep) oldEnough(stamp string) bool {
	at, err := time.Parse(time.RFC3339Nano, stamp)
	return err == nil && at.Year() >= 2000 && !at.After(s.before)
}

func stoppedState(status string) bool {
	return status == "exited" || status == "dead" || status == "created"
}

func tagged(tags []string) bool {
	for _, tag := range tags {
		if tag != "" && tag != "<none>:<none>" {
			return true
		}
	}
	return false
}

func summaryLine(output string, err error, prefix string) string {
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return strings.Join(strings.Fields(line), " ")
		}
	}
	return ""
}

// resourceOwners answers ownerLookup from Pier's own tables. Unknown types
// (one-off helpers) have no owner; a lookup error keeps the container.
type resourceOwners struct{ db *sql.DB }

var ownerQueries = map[string]string{
	docker.TypeApplication: `SELECT EXISTS(SELECT 1 FROM applications WHERE id = ?1) OR EXISTS(SELECT 1 FROM application_previews WHERE id = ?1)`,
	docker.TypeDatabase:    `SELECT EXISTS(SELECT 1 FROM databases WHERE id = ?1)`,
	docker.TypeStack:       `SELECT EXISTS(SELECT 1 FROM stacks WHERE id = ?1)`,
	docker.TypeCluster:     `SELECT EXISTS(SELECT 1 FROM clusters WHERE id = ?1)`,
}

func (r resourceOwners) exists(ctx context.Context, resourceType, id string) (bool, error) {
	query, known := ownerQueries[resourceType]
	if !known {
		return false, nil
	}
	if id == "" {
		return true, nil
	}
	var found bool
	err := r.db.QueryRowContext(ctx, query, id).Scan(&found)
	return found, err
}
