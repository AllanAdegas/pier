package application

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"pier/internal/platform/executor"
)

// gitSourceExecutor replaces only Docker's container boundary: the actual Git
// clone script runs against an authenticated loopback Git HTTP backend.
type gitSourceExecutor struct {
	executor.Executor
	buildDir, home string
}

func (e gitSourceExecutor) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	if !strings.Contains(command, gitImage) {
		return e.Executor.Stream(ctx, command, sink)
	}
	info, err := os.Stat(filepath.Join(e.buildDir, "clone.env"))
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o600 {
		return fmt.Errorf("clone env permissions: %o", info.Mode().Perm())
	}
	data, err := os.ReadFile(filepath.Join(e.buildDir, "clone.env"))
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", strings.ReplaceAll(cloneScript, "/work/src", filepath.Join(e.buildDir, "src")))
	cmd.Env = append(os.Environ(), "HOME="+e.home, "GIT_CONFIG_GLOBAL="+filepath.Join(e.home, "gitconfig"), "GIT_CONFIG_NOSYSTEM=1")
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		cmd.Env = append(cmd.Env, line)
	}
	output, err := cmd.CombinedOutput()
	for _, line := range strings.Split(string(output), "\n") {
		sink(line)
	}
	return err
}

func gitFixtureObject(t *testing.T, dir, kind string, content []byte) []byte {
	t.Helper()
	raw := append([]byte(fmt.Sprintf("%s %d\x00", kind, len(content))), content...)
	digest := sha1.Sum(raw)
	name := hex.EncodeToString(digest[:])
	filename := filepath.Join(dir, "objects", name[:2], name[2:])
	if err := os.MkdirAll(filepath.Dir(filename), 0o700); err != nil {
		t.Fatal(err)
	}
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	if _, err := writer.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filename, compressed.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return digest[:]
}

// Create immutable Git test objects directly, without changing any working
// copy's Git state or invoking git init/commit/update-ref.
func gitFixture(t *testing.T, dir string, child []byte) []byte {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "refs", "heads"), 0o700); err != nil {
		t.Fatal(err)
	}
	var tree []byte
	if child != nil {
		modules := gitFixtureObject(t, dir, "blob", []byte("[submodule \"child\"]\n\tpath = child\n\turl = ../child.git\n"))
		tree = append(tree, []byte("100644 .gitmodules\x00")...)
		tree = append(tree, modules...)
		tree = append(tree, []byte("160000 child\x00")...)
		tree = append(tree, child...)
	} else {
		blob := gitFixtureObject(t, dir, "blob", []byte("test file\n"))
		tree = append(tree, []byte("100644 file.txt\x00")...)
		tree = append(tree, blob...)
	}
	root := gitFixtureObject(t, dir, "tree", tree)
	commit := gitFixtureObject(t, dir, "commit", []byte(fmt.Sprintf("tree %x\nauthor Test <test@example.com> 1700000000 +0000\ncommitter Test <test@example.com> 1700000000 +0000\n\nfixture\n", root)))
	for name, content := range map[string]string{"HEAD": "ref: refs/heads/main\n", "refs/heads/main": fmt.Sprintf("%x\n", commit), "config": "[core]\n\tbare = true\n\trepositoryformatversion = 0\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return commit
}

func TestSourceFetchNeverPersistsHTTPSCredentialsAndClonesRelativeSubmodules(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required for the clone regression test")
	}
	root := t.TempDir()
	child := gitFixture(t, filepath.Join(root, "child.git"), nil)
	gitFixture(t, filepath.Join(root, "main.git"), child)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if strings.HasPrefix(r.URL.Path, "/child.git/") {
			if r.Header.Get("Authorization") != "" {
				t.Error("parent credential leaked to submodule")
			}
		} else if !ok || user != "user" || password != "credential-for-test" {
			w.Header().Set("WWW-Authenticate", `Basic realm="test"`)
			w.WriteHeader(401)
			return
		}
		command := exec.Command(git, "http-backend")
		command.Env = append(os.Environ(), "GIT_PROJECT_ROOT="+root, "GIT_HTTP_EXPORT_ALL=1", "PATH_INFO="+r.URL.Path, "REQUEST_METHOD="+r.Method, "QUERY_STRING="+r.URL.RawQuery, "CONTENT_TYPE="+r.Header.Get("Content-Type"), fmt.Sprintf("CONTENT_LENGTH=%d", r.ContentLength), "REMOTE_USER=user")
		command.Stdin = r.Body
		output, err := command.Output()
		if err != nil {
			t.Errorf("Git HTTP backend: %v", err)
			w.WriteHeader(500)
			return
		}
		reader := bufio.NewReader(bytes.NewReader(output))
		headers, err := textproto.NewReader(reader).ReadMIMEHeader()
		if err != nil {
			t.Errorf("Git HTTP headers: %v", err)
			w.WriteHeader(500)
			return
		}
		for key, values := range headers {
			if key != "Status" {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
		}
		if _, err := io.Copy(w, reader); err != nil {
			t.Errorf("Git HTTP body: %v", err)
		}
	}))
	defer backend.Close()
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.server.DataDir = t.TempDir()
	j.app.Config.Source.Repository = strings.Replace(backend.URL, "http://", "http://user:credential-for-test@", 1) + "/main.git"
	j.app.Config.Source.Branch = "main"
	buildDir := j.server.BuildDir(j.deployment.ID)
	j.ex = gitSourceExecutor{Executor: executor.NewLocal(), buildDir: buildDir, home: t.TempDir()}
	sha, _, err := j.fetchSource(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if sha == "" {
		t.Fatal("clone did not report its commit")
	}
	for _, name := range []string{".git/config", ".git/modules/child/config"} {
		data, err := os.ReadFile(filepath.Join(buildDir, "src", name))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "credential-for-test") || strings.Contains(string(data), "Authorization") || strings.Contains(string(data), "user@") {
			t.Fatalf("credential persisted in %s: %s", name, data)
		}
	}
	if _, err := os.Stat(filepath.Join(buildDir, "src", "child", "file.txt")); err != nil {
		t.Fatalf("relative submodule not cloned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "clone.env")); !os.IsNotExist(err) {
		t.Fatalf("clone credentials were not removed: %v", err)
	}
}

func TestSourceNestedSymlinksAreCheckedOnTheExecutor(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.server.DataDir = t.TempDir()
	j.app.Config.Build.Pack = PackNixpacks
	j.ex = executor.NewLocal()
	checkout := path.Join(j.server.BuildDir(j.deployment.ID), sourceDir)
	if err := os.MkdirAll(filepath.Join(checkout, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "inside"), []byte("safe"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(checkout, "nested", "file with spaces")
	if err := os.Symlink("../inside", link); err != nil {
		t.Fatal(err)
	}
	if err := j.ensureInsideSource(context.Background()); err != nil {
		t.Fatalf("valid internal link: %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	if err := j.ensureInsideSource(context.Background()); err == nil || !strings.Contains(err.Error(), link) {
		t.Fatalf("external nested link not named: %v", err)
	}
}

func TestSourceCloneUsesPrivateEnvFileAndChecksCredentialRemoval(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	j.app.Config.Source.Repository = "https://user:credential-for-test@example.com/repo"
	fixture.ex.On(gitImage, "PIER_COMMIT deadbeef fixture", 0)
	if _, _, err := j.fetchSource(context.Background(), "private-key-for-test"); err != nil {
		t.Fatal(err)
	}
	clone := fixture.ex.Commands[indexOf(fixture.ex.Commands, gitImage)]
	if strings.Contains(clone, "credential-for-test") || strings.Contains(clone, "PIER_REPOSITORY=https") || !strings.Contains(clone, "--env-file") || strings.Contains(clone, "UserKnownHostsFile=/dev/null") {
		t.Fatalf("unsafe clone command: %s", clone)
	}
	env := string(fixture.ex.Files[path.Join(j.server.BuildDir(j.deployment.ID), "clone.env")])
	if !strings.Contains(env, "PIER_REPOSITORY=https://example.com/repo") || !strings.Contains(env, "GIT_CONFIG_VALUE_0=Authorization: Basic ") {
		t.Fatalf("incorrect ephemeral authentication: %s", env)
	}
	if !strings.Contains(clone, j.server.ResourceDir("application", j.app.ID)+"/git:/trust") || !strings.Contains(clone, "trust=yes") || !strings.Contains(clone, "trust=accept-new") {
		t.Fatalf("host trust not persisted: %s", clone)
	}
	fixture.ex.On("rm -f", "permission denied", 1)
	if _, _, err := j.fetchSource(context.Background(), "private-key-for-test"); err == nil || !strings.Contains(err.Error(), "credenciais temporárias") {
		t.Fatalf("ignored failed key removal: %v", err)
	}
}

func TestSourceGitHostKeyMismatchFailsClearly(t *testing.T) {
	fixture := newDeployFixture(t, gitConfig())
	j := sourceTestJob(fixture)
	fixture.ex.On(gitImage, "REMOTE HOST IDENTIFICATION HAS CHANGED!", 1)
	if _, _, err := j.fetchSource(context.Background(), "private-key-for-test"); err == nil || !strings.Contains(err.Error(), "redefina a confiança Git") {
		t.Fatalf("unclear Git host key error: %v", err)
	}
	service := NewService(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil)
	if err := service.ResetGitHostKey(context.Background(), fixture.app.ID); err != nil {
		t.Fatal(err)
	}
	if !fixture.ex.Ran("/git/known_hosts") {
		t.Error("reset did not remove the stored pin")
	}
}
