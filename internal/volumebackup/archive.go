package volumebackup

import (
	"path"

	"pier/internal/database"
	"pier/internal/platform/shell"
)

const (
	sourceRoot  = "/source"
	restoreRoot = "/restore"
	archiveDir  = "/out"
	inputDir    = "/in"

	helperBackupPrefix  = "pier-volbackup-"
	helperRestorePrefix = "pier-volrestore-"
)

// restoreScript wipes a named volume (not host directories: those are only
// overwritten) and extracts the label's entries over it. The label and file
// arrive as positional arguments, never spliced into the script.
const restoreScript = `set -e
if [ "$3" = wipe ]; then find "/restore/$1" -mindepth 1 -delete; fi
tar -xzf "/in/$2" -C /restore "$1"`

func mountFlag(mount Mount, destination string, readOnly bool) []string {
	spec := "type=" + mount.Kind + ",src=" + mount.Source + ",dst=" + destination
	if readOnly {
		spec += ",readonly"
	}
	return []string{"--mount", spec}
}

// backupCommand runs the helper that archives the mounts read-only. Mounts
// are exposed as /source/<label>; the archive keeps one directory per label.
func backupCommand(helper, directory, file string, mounts []Mount) string {
	args := []string{"docker", "run", "--rm", "--name", helper, "--network", "none",
		"--security-opt", "no-new-privileges"}
	for _, mount := range mounts {
		args = append(args, mountFlag(mount, path.Join(sourceRoot, mount.Label), true)...)
	}
	args = append(args, "-v", directory+":"+archiveDir, database.HelperImage,
		"tar", "-czf", path.Join(archiveDir, file), "-C", sourceRoot)
	for _, mount := range mounts {
		args = append(args, mount.Label)
	}
	return shell.Join(args...)
}

// restoreCommand extracts one label of the archive into its mount.
func restoreCommand(helper, directory, file string, mount Mount) string {
	mode := "keep"
	if mount.Kind == MountVolume {
		mode = "wipe"
	}
	args := []string{"docker", "run", "--rm", "--name", helper, "--network", "none",
		"--security-opt", "no-new-privileges"}
	args = append(args, mountFlag(mount, path.Join(restoreRoot, mount.Label), false)...)
	args = append(args, "-v", directory+":"+inputDir+":ro", database.HelperImage,
		"sh", "-c", restoreScript, "sh", mount.Label, file, mode)
	return shell.Join(args...)
}
