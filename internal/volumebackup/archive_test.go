package volumebackup

import (
	"strings"
	"testing"

	"pier/internal/database"
)

func TestBackupCommandMountsReadOnlyWithAPinnedImage(t *testing.T) {
	mounts := []Mount{
		{Label: "app1_data", Kind: MountVolume, Source: "app1_data"},
		{Label: "bind-abc", Kind: MountBind, Source: "/data/pier/applications/app1/uploads"},
	}
	command := backupCommand("pier-volbackup-x", "/data/pier/backups/volumes-app1", "f.tar.gz", mounts)
	for _, want := range []string{
		"docker run --rm --name pier-volbackup-x --network none --security-opt no-new-privileges",
		"--mount type=volume,src=app1_data,dst=/source/app1_data,readonly",
		"--mount type=bind,src=/data/pier/applications/app1/uploads,dst=/source/bind-abc,readonly",
		"-v /data/pier/backups/volumes-app1:/out " + database.HelperImage,
		"tar -czf /out/f.tar.gz -C /source app1_data bind-abc",
	} {
		if !strings.Contains(command, want) {
			t.Errorf("missing %q in:\n%s", want, command)
		}
	}
	if strings.Contains(database.HelperImage, "latest") || !strings.Contains(database.HelperImage, ":3.") {
		t.Errorf("the helper image must be pinned: %s", database.HelperImage)
	}
}

func TestCommandsQuoteEveryDynamicValue(t *testing.T) {
	hostile := Mount{Label: "x; rm -rf /", Kind: MountBind, Source: "/data/pier/a b$(id)"}
	backup := backupCommand("h", "/d ir", "f $(x).tar.gz", []Mount{hostile})
	restore := restoreCommand("h", "/d ir", "f $(x).tar.gz", hostile)
	for _, want := range []string{"'type=bind,src=/data/pier/a b$(id),dst=/source/x; rm -rf ,readonly'",
		"'/d ir:/out'", "'/out/f $(x).tar.gz'", "'x; rm -rf /'"} {
		if !strings.Contains(backup, want) {
			t.Errorf("backup command lacks %s:\n%s", want, backup)
		}
	}
	for _, want := range []string{"'type=bind,src=/data/pier/a b$(id),dst=/restore/x; rm -rf '", "'f $(x).tar.gz'", "'x; rm -rf /'"} {
		if !strings.Contains(restore, want) {
			t.Errorf("restore command lacks %s:\n%s", want, restore)
		}
	}
	if strings.Contains(restore, `"$1"' sh`) == false {
		t.Errorf("the restore script must take label and file as arguments:\n%s", restore)
	}
}

func TestRestoreCommandWipesVolumesButOnlyOverwritesHostDirectories(t *testing.T) {
	volume := restoreCommand("h", "/backups", "f.tar.gz", Mount{Label: "data", Kind: MountVolume, Source: "data"})
	bind := restoreCommand("h", "/backups", "f.tar.gz", Mount{Label: "bind-1", Kind: MountBind, Source: "/data/pier/x"})
	if !strings.HasSuffix(volume, " sh data f.tar.gz wipe") || !strings.HasSuffix(bind, " sh bind-1 f.tar.gz keep") {
		t.Errorf("modes:\n%s\n%s", volume, bind)
	}
	if !strings.Contains(volume, "-v /backups:/in:ro") || !strings.Contains(volume, "type=volume,src=data,dst=/restore/data") {
		t.Errorf("restore mounts: %s", volume)
	}
	if strings.Contains(volume, "dst=/restore/data,readonly") {
		t.Errorf("the target must be writable: %s", volume)
	}
}
