package application

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"pier/internal/platform/apperr"
)

type tarEntry struct {
	name, body, link string
	kind             byte
	mode             int64
}

func tarGz(t *testing.T, entries ...tarEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zipped := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zipped)
	for _, entry := range entries {
		kind := entry.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := entry.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{Name: entry.name, Typeflag: kind, Mode: mode, Linkname: entry.link, Size: int64(len(entry.body)), Uid: 1000}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			_, _ = writer.Write([]byte(entry.body))
		}
	}
	_ = writer.Close()
	_ = zipped.Close()
	return buffer.Bytes()
}

func untar(t *testing.T, archive []byte) map[string]*tar.Header {
	t.Helper()
	zipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(zipped)
	headers := map[string]*tar.Header{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			return headers
		}
		if err != nil {
			t.Fatal(err)
		}
		headers[header.Name] = header
	}
}

func TestSanitizedArchivesKeepSafeEntriesWithNeutralOwnership(t *testing.T) {
	archive := tarGz(t,
		tarEntry{name: "./", kind: tar.TypeDir},
		tarEntry{name: "src/", kind: tar.TypeDir},
		tarEntry{name: "src/main.go", body: "package main"},
		tarEntry{name: "run.sh", body: "echo", mode: 0o4755},
		tarEntry{name: "node_modules/.bin/tool", kind: tar.TypeSymlink, link: "../tool/bin.js"},
		tarEntry{name: "copy.go", kind: tar.TypeLink, link: "src/main.go"},
	)
	var out bytes.Buffer
	summary, err := sanitizeArchive(bytes.NewReader(archive), &out)
	if err != nil {
		t.Fatalf("sanitize: %v", err)
	}
	if summary.Files != 2 || summary.Bytes != int64(len("package main")+len("echo")) {
		t.Errorf("summary = %+v", summary)
	}
	headers := untar(t, out.Bytes())
	names := []string{}
	for name := range headers {
		names = append(names, name)
	}
	sort.Strings(names)
	if strings.Join(names, ",") != "copy.go,node_modules/.bin/tool,run.sh,src/,src/main.go" {
		t.Fatalf("entries = %v", names)
	}
	if headers["run.sh"].Mode != 0o755 || headers["run.sh"].Uid != 0 {
		t.Errorf("special bits or ownership kept: mode=%o uid=%d", headers["run.sh"].Mode, headers["run.sh"].Uid)
	}
}

func TestUnsafeArchivesAreRejectedWhole(t *testing.T) {
	cases := map[string][]tarEntry{
		"absolute path":          {{name: "/etc/passwd", body: "x"}},
		"parent segment":         {{name: "src/../../escape", body: "x"}},
		"absolute symlink":       {{name: "link", kind: tar.TypeSymlink, link: "/etc"}},
		"escaping symlink":       {{name: "a/link", kind: tar.TypeSymlink, link: "../../etc"}},
		"symlink through link":   {{name: "sub", kind: tar.TypeSymlink, link: "."}, {name: "sub2/l", kind: tar.TypeSymlink, link: "../sub/.."}},
		"chained symlinks":       {{name: "a/b/l1", kind: tar.TypeSymlink, link: "../.."}, {name: "a/b/l2", kind: tar.TypeSymlink, link: "l1/../.."}},
		"write through symlink":  {{name: "dir", kind: tar.TypeSymlink, link: "real"}, {name: "dir/file", body: "x"}},
		"symlink over directory": {{name: "dir/file", body: "x"}, {name: "dir", kind: tar.TypeSymlink, link: "real"}},
		"hardlink outside":       {{name: "f", body: "x"}, {name: "h", kind: tar.TypeLink, link: "../f"}},
		"hardlink to unknown":    {{name: "h", kind: tar.TypeLink, link: "/etc/shadow"}},
		"device":                 {{name: "dev", kind: tar.TypeChar}},
		"fifo":                   {{name: "pipe", kind: tar.TypeFifo}},
		"duplicate":              {{name: "f", body: "x"}, {name: "f", body: "y"}},
		"empty":                  {{name: "dir/", kind: tar.TypeDir}},
	}
	for name, entries := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := sanitizeArchive(bytes.NewReader(tarGz(t, entries...)), io.Discard)
			if appErr, ok := apperr.As(err); !ok || appErr.Kind != apperr.KindInvalid {
				t.Fatalf("expected a validation error, got %v", err)
			}
		})
	}
}

func TestArchivesThatAreNotGzipOrTooLargeAreRejected(t *testing.T) {
	if _, err := sanitizeArchive(strings.NewReader("plain text"), io.Discard); err == nil || !strings.Contains(err.Error(), "tar.gz") {
		t.Errorf("not gzip: %v", err)
	}
	limited := &limitedReader{reader: strings.NewReader("abcdef"), remaining: 3}
	if data, err := io.ReadAll(limited); err == nil || !limited.exceeded || string(data) != "abc" {
		t.Errorf("limit not enforced: %q %v", data, err)
	}
	exact := &limitedReader{reader: strings.NewReader("abc"), remaining: 3}
	if _, err := io.ReadAll(exact); err != nil || exact.exceeded {
		t.Errorf("exact size must pass: %v", err)
	}
}

func archiveFixture(t *testing.T) (*deployFixture, *Service, string) {
	t.Helper()
	fixture := newDeployFixture(t, gitConfig())
	dir := filepath.Join(t.TempDir(), "archives")
	fixture.deployer.SetArchiveDir(dir)
	service := NewService(fixture.store, fixture.env, fakeConnector{fixture.ex},
		NewDispatcher(fixture.store, fixture.deployer, fixture.deployer.logger, time.Hour))
	return fixture, service, dir
}

func TestAnUploadedArchiveIsBuiltWithoutGitAndDeletedAfterwards(t *testing.T) {
	fixture, service, dir := archiveFixture(t)
	fixture.ex.On("docker inspect --format '{{.State.Status}}", "running|healthy|0", 0)
	ctx := context.Background()
	queued, err := service.DeployArchive(ctx, fixture.app.ID, bytes.NewReader(tarGz(t, tarEntry{name: "Dockerfile", body: "FROM scratch"})))
	if err != nil {
		t.Fatalf("deploy archive: %v", err)
	}
	if queued.Trigger != TriggerArchive || !strings.Contains(queued.Source, "1 arquivos") {
		t.Fatalf("queued = %+v", queued)
	}
	stored := filepath.Join(dir, queued.ID+archiveSuffix)
	if info, err := os.Stat(stored); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive not stored privately: %v %v", info, err)
	}
	if claimed, err := fixture.store.claimDeployment(ctx, queued.ID); err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	fixture.deployer.Run(ctx, queued.ID)

	if fixture.ex.Ran("git clone") {
		t.Error("an archive deployment must not clone the repository")
	}
	target := "/data/pier/builds/" + queued.ID + "/" + sourceArchive
	upload := fixture.ex.Inputs["cat >> "+target]
	if _, err := sanitizeArchive(bytes.NewReader(upload), io.Discard); err != nil {
		t.Errorf("the server did not receive the sanitized archive: %v", err)
	}
	extract := indexOf(fixture.ex.Commands, "tar -xzf")
	build := indexOf(fixture.ex.Commands, "docker buildx build")
	if extract == -1 || build == -1 || extract > build || !strings.Contains(fixture.ex.Commands[extract], "--network none") {
		t.Errorf("expected an isolated extraction before the build:\n%s", strings.Join(fixture.ex.Commands, "\n"))
	}
	if _, err := os.Stat(stored); !os.IsNotExist(err) {
		t.Errorf("the archive must be deleted after the deployment: %v", err)
	}
}

func TestArchiveDeploysRespectTheGuardAndImageApplications(t *testing.T) {
	fixture, service, _ := archiveFixture(t)
	service.SetDeployGuard(frozenGuard{})
	body := bytes.NewReader(tarGz(t, tarEntry{name: "a", body: "b"}))
	if _, err := service.DeployArchive(context.Background(), fixture.app.ID, body); err == nil || !strings.Contains(err.Error(), "congelado") {
		t.Errorf("frozen environment must refuse uploads: %v", err)
	}
	image := newDeployFixture(t, imageConfig())
	image.deployer.SetArchiveDir(t.TempDir())
	imageService := NewService(image.store, image.env, fakeConnector{image.ex},
		NewDispatcher(image.store, image.deployer, image.deployer.logger, time.Hour))
	if _, err := imageService.DeployArchive(context.Background(), image.app.ID, body); err == nil {
		t.Error("image applications cannot receive source archives")
	}
}

func TestAQueuedArchiveIsDeletedWhenCancelledAndNeverJoinsAnotherDeploy(t *testing.T) {
	fixture, service, dir := archiveFixture(t)
	ctx := context.Background()
	upload, err := service.DeployArchive(ctx, fixture.app.ID, bytes.NewReader(tarGz(t, tarEntry{name: "a", body: "b"})))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := service.Deploy(ctx, fixture.app.ID, TriggerManual)
	if err != nil || plain.ID == upload.ID {
		t.Fatalf("a plain deploy must not reuse a queued upload: %v %v", plain.ID, err)
	}
	if err := service.CancelDeployment(ctx, upload.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, upload.ID+archiveSuffix)); !os.IsNotExist(err) {
		t.Errorf("cancelled upload still stored: %v", err)
	}
}

type frozenGuard struct{}

func (frozenGuard) CheckDeploy(context.Context, string) error {
	return apperr.Conflict("Ambiente congelado.")
}
