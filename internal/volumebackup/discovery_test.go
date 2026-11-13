package volumebackup

import (
	"context"
	"strings"
	"testing"

	"pier/internal/platform/executor"
)

func TestDiscoveryClassifiesVolumesAndBindMounts(t *testing.T) {
	fake := executor.NewFake().On("docker ps", psLine, 0).On("docker inspect", mountsLine, 0)
	mounts, containers, err := discoverMounts(context.Background(), fake, "app1", dataDir, Policy{})
	if err != nil || len(containers) != 1 {
		t.Fatalf("discoverMounts: %v (%d containers)", err, len(containers))
	}
	byDestination := map[string]Mount{}
	for _, mount := range mounts {
		byDestination[mount.Destination] = mount
	}
	if len(mounts) != 4 {
		t.Fatalf("expected four mounts, got %+v", mounts)
	}
	if m := byDestination["/var/run/docker.sock"]; m.Included || m.Reason != ReasonSystemPath {
		t.Errorf("the docker socket must be listed but excluded: %+v", m)
	}
	if m := byDestination["/data"]; !m.Included || m.Kind != MountVolume || m.Label != "app1_data" {
		t.Errorf("named volume: %+v", m)
	}
	if m := byDestination["/uploads"]; !m.Included || m.Kind != MountBind || !strings.HasPrefix(m.Label, "bind-") {
		t.Errorf("bind under the data dir must be included: %+v", m)
	}
	if m := byDestination["/shared"]; m.Included || m.Reason != ReasonOutsideDataDir {
		t.Errorf("bind outside the data dir must be listed but excluded: %+v", m)
	}
}

func TestExternalBindsAreIncludedOnlyWhenThePolicyAsks(t *testing.T) {
	mounts, err := classifyMounts(mountsLine, dataDir, Policy{IncludeExternal: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, mount := range mounts {
		if mount.Destination == "/shared" && !mount.Included {
			t.Errorf("external bind should be included with the policy: %+v", mount)
		}
	}
}

func TestSystemPathsAreNeverIncluded(t *testing.T) {
	for _, source := range []string{"/", "/etc", "/etc/ssl", "/proc", "/var/lib/docker/volumes", "/data/pier",
		"/data/pier/backups/x", "/run/user/1000/bus.sock", "/var/run/docker.sock"} {
		line := `[{"Type":"bind","Source":"` + source + `","Destination":"/x"}]`
		mounts, err := classifyMounts(line, dataDir, Policy{IncludeExternal: true})
		if err != nil {
			t.Fatal(err)
		}
		for _, mount := range mounts {
			if mount.Included {
				t.Errorf("%s must never be archived: %+v", source, mount)
			}
		}
	}
}

func TestOddVolumeNamesAreExcludedAndDuplicatesCollapse(t *testing.T) {
	line := `[{"Type":"volume","Name":"a;touch x","Destination":"/a"},{"Type":"volume","Name":"data","Destination":"/d"},` +
		`{"Type":"volume","Name":"data","Destination":"/e"},{"Type":"tmpfs","Destination":"/t"}]`
	mounts, err := classifyMounts(line+"\n"+line, dataDir, Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(mounts) != 2 {
		t.Fatalf("expected one odd and one regular volume, got %+v", mounts)
	}
	for _, mount := range mounts {
		if mount.Source == "a;touch x" && (mount.Included || mount.Reason != ReasonInvalidName) {
			t.Errorf("odd name must be excluded: %+v", mount)
		}
	}
}

func TestDiscoveryOfAResourceWithoutContainersIsEmpty(t *testing.T) {
	mounts, containers, err := discoverMounts(context.Background(), executor.NewFake(), "app1", dataDir, Policy{})
	if err != nil || len(mounts) != 0 || len(containers) != 0 {
		t.Fatalf("got %v %v %v", mounts, containers, err)
	}
}
