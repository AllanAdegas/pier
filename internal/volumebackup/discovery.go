package volumebackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

// systemPaths are never archived, whatever the policy says: they are not
// application data, and some of them would hand the helper the host.
var systemPaths = []string{"/", "/proc", "/sys", "/dev", "/run", "/var/run", "/var/lib/docker", "/etc", "/boot"}

type inspectedMount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

// discoverMounts lists the persistent mounts of the containers of a resource
// (running or not) and decides which ones go into the archive. Containers are
// returned too, for the callers that stop and start them.
func discoverMounts(ctx context.Context, ex executor.Executor, resourceID, dataDir string, policy Policy) ([]Mount, []docker.Container, error) {
	containers, err := docker.ListByResource(ctx, ex, resourceID)
	if err != nil {
		return nil, nil, err
	}
	if len(containers) == 0 {
		return nil, nil, nil
	}
	args := []string{"docker", "inspect", "--format", "{{json .Mounts}}"}
	for _, container := range containers {
		args = append(args, container.ID)
	}
	output, err := ex.Run(ctx, shell.Join(args...))
	if err != nil {
		return nil, nil, fmt.Errorf("inspect containers: %w", err)
	}
	mounts, err := classifyMounts(output, dataDir, policy)
	return mounts, containers, err
}

// classifyMounts parses one JSON array per line (one per container).
func classifyMounts(output, dataDir string, policy Policy) ([]Mount, error) {
	seen := map[string]bool{}
	mounts := []Mount{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var inspected []inspectedMount
		if err := json.Unmarshal([]byte(line), &inspected); err != nil {
			return nil, fmt.Errorf("parse mounts: %w", err)
		}
		for _, raw := range inspected {
			mount, keep := classify(raw, dataDir, policy)
			if keep && !seen[mount.Label] {
				seen[mount.Label] = true
				mounts = append(mounts, mount)
			}
		}
	}
	sort.Slice(mounts, func(i, j int) bool { return mounts[i].Label < mounts[j].Label })
	return mounts, nil
}

func classify(raw inspectedMount, dataDir string, policy Policy) (Mount, bool) {
	switch raw.Type {
	case MountVolume:
		mount := Mount{Label: raw.Name, Kind: MountVolume, Source: raw.Name, Destination: raw.Destination, Included: true}
		if !volumeNamePattern.MatchString(raw.Name) {
			mount.Included, mount.Reason = false, ReasonInvalidName
		}
		return mount, raw.Name != ""
	case MountBind:
		source := path.Clean(raw.Source)
		mount := Mount{Label: bindLabel(source), Kind: MountBind, Source: source, Destination: raw.Destination}
		mount.Included, mount.Reason = bindDecision(source, dataDir, policy)
		return mount, path.IsAbs(source)
	}
	return Mount{}, false
}

func bindDecision(source, dataDir string, policy Policy) (bool, string) {
	if isSystemPath(source, dataDir) || strings.ContainsAny(source, "\n\r\x00:,") {
		return false, ReasonSystemPath
	}
	if within(source, dataDir) || policy.IncludeExternal {
		return true, ""
	}
	return false, ReasonOutsideDataDir
}

func isSystemPath(source, dataDir string) bool {
	if strings.HasSuffix(source, ".sock") || source == path.Clean(dataDir) || within(source, path.Join(dataDir, "backups")) {
		return true
	}
	for _, system := range systemPaths {
		if source == system || (system != "/" && within(source, system)) {
			return true
		}
	}
	return false
}

func within(source, directory string) bool {
	directory = path.Clean(directory)
	return directory != "." && strings.HasPrefix(source, strings.TrimSuffix(directory, "/")+"/")
}

// bindLabel names a host directory inside the archive without leaking or
// trusting the path: the label is a short hash.
func bindLabel(source string) string {
	sum := sha256.Sum256([]byte(source))
	return "bind-" + hex.EncodeToString(sum[:6])
}

func included(mounts []Mount) []Mount {
	result := []Mount{}
	for _, mount := range mounts {
		if mount.Included {
			result = append(result, mount)
		}
	}
	return result
}
