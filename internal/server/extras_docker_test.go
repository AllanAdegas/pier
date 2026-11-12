package server

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/executor"
)

// installHost scripts a server where Docker answers only after the install.
type installHost struct {
	*executor.Fake
	mu        sync.Mutex
	installed bool
}

func (h *installHost) Run(ctx context.Context, command string) (string, error) {
	h.mu.Lock()
	installed := h.installed
	h.mu.Unlock()
	if strings.HasPrefix(command, "docker version") || strings.HasPrefix(command, "docker compose version") {
		h.Fake.Commands = append(h.Fake.Commands, command)
		if !installed {
			return "", &executor.ExitError{Command: command, Code: 127}
		}
		if strings.Contains(command, "compose") {
			return "2.29.1", nil
		}
		return "27.3.1", nil
	}
	return h.Fake.Run(ctx, command)
}

func (h *installHost) Stream(ctx context.Context, command string, sink executor.LineSink) error {
	err := h.Fake.Stream(ctx, command, sink)
	if err == nil && (strings.Contains(command, "get.docker.com") || strings.Contains(command, "install -y")) {
		h.mu.Lock()
		h.installed = true
		h.mu.Unlock()
	}
	return err
}

type installConnector struct {
	ex        executor.Executor
	target    Server
	forgotten int
}

func (c *installConnector) Executor(context.Context, string) (executor.Executor, Server, error) {
	return c.ex, c.target, nil
}
func (c *installConnector) Forget(string) { c.forgotten++ }

func ubuntuHost(uid string) *installHost {
	fake := executor.NewFake().On("uname -s", "Linux", 0).
		On("cat /etc/os-release", "NAME=\"Ubuntu\"\nID=ubuntu\nPRETTY_NAME=\"Ubuntu 24.04.1 LTS\"\nVERSION_ID=\"24.04\"", 0).
		On("id -u", uid, 0)
	return &installHost{Fake: fake}
}

func sshTarget() Server { return Server{ID: "vm", Name: "vm-1", Kind: KindSSH, User: "root"} }

func TestDockerInstallWithConvenienceScriptAsRootVerifiesVersions(t *testing.T) {
	host := ubuntuHost("0")
	connector := &installConnector{ex: host, target: sshTarget()}
	validated := 0
	installer := NewDockerInstaller(connector, func(context.Context, string) (Server, error) { validated++; return Server{}, nil })
	lines := []string{}
	result, err := installer.Install(context.Background(), "vm", DockerConvenience, func(line string) { lines = append(lines, line) })
	if err != nil {
		t.Fatal(err)
	}
	if result.DockerVersion != "27.3.1" || result.ComposeVersion != "2.29.1" || result.System != "Ubuntu 24.04.1 LTS" || validated != 1 {
		t.Fatalf("result %+v validated=%d", result, validated)
	}
	if !host.Ran("https://get.docker.com") || host.Ran("sudo") || host.Ran("usermod") || connector.forgotten == 0 {
		t.Fatalf("commands %v forgotten=%d", host.Commands, connector.forgotten)
	}
	if !strings.Contains(strings.Join(lines, "\n"), "$ script=$(mktemp)") {
		t.Fatalf("output not streamed: %v", lines)
	}
}

func TestDockerInstallWithPackagesUsesSudoAndDockerGroup(t *testing.T) {
	host := ubuntuHost("1000")
	host.Fake.On("sudo -n true", "", 0)
	target := sshTarget()
	target.User = "deploy"
	installer := NewDockerInstaller(&installConnector{ex: host, target: target}, nil)
	if _, err := installer.Install(context.Background(), "vm", DockerPackages, func(string) {}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sudo -n env DEBIAN_FRONTEND=noninteractive apt-get install -y -q docker.io docker-compose-v2", "sudo -n usermod -aG docker deploy", "sudo -n systemctl enable --now docker"} {
		if !host.Ran(want) {
			t.Fatalf("missing %q in %v", want, host.Commands)
		}
	}
	if host.Ran("get.docker.com") {
		t.Fatal("packages method downloaded the script")
	}
}

func TestDockerInstallRefusesWhatItCannotDoBeforeRunningAnything(t *testing.T) {
	for _, tc := range []struct {
		name, method, want string
		host               *installHost
	}{
		{"macOS", DockerConvenience, "exige Linux", &installHost{Fake: executor.NewFake().On("uname -s", "Darwin", 0)}},
		{"alpine script", DockerConvenience, "não suporta Alpine", &installHost{Fake: executor.NewFake().On("uname -s", "Linux", 0).On("os-release", "ID=alpine\nPRETTY_NAME=\"Alpine Linux v3.20\"", 0)}},
		{"centos packages", DockerPackages, "Não há pacotes", &installHost{Fake: executor.NewFake().On("uname -s", "Linux", 0).On("os-release", "ID=centos\nPRETTY_NAME=\"CentOS Stream 9\"", 0)}},
		{"no sudo", DockerConvenience, "sudo sem senha", func() *installHost {
			h := ubuntuHost("1000")
			h.Fake.On("sudo -n true", "password required", 1)
			return h
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			installer := NewDockerInstaller(&installConnector{ex: tc.host, target: sshTarget()}, nil)
			_, err := installer.Install(context.Background(), "vm", tc.method, func(string) {})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v", err)
			}
			if tc.host.Ran("get.docker.com") || tc.host.Ran("apt-get") || tc.host.Ran("dnf") {
				t.Fatalf("ran install: %v", tc.host.Commands)
			}
		})
	}
}

func TestDockerInstallFailsWhenComposeIsMissingAfterInstall(t *testing.T) {
	host := ubuntuHost("0")
	installer := NewDockerInstaller(&installConnector{ex: &composeLess{host}, target: sshTarget()}, nil)
	if _, err := installer.Install(context.Background(), "vm", DockerPackages, func(string) {}); err == nil || !strings.Contains(err.Error(), "Compose") {
		t.Fatalf("err %v", err)
	}
}

type composeLess struct{ *installHost }

func (c *composeLess) Run(ctx context.Context, command string) (string, error) {
	if strings.HasPrefix(command, "docker compose version") {
		return "", &executor.ExitError{Command: command, Code: 1}
	}
	return c.installHost.Run(ctx, command)
}

func TestDockerInstallStartRequiresTypedNameAndRunsInBackground(t *testing.T) {
	host := ubuntuHost("0")
	connector := &installConnector{ex: host, target: sshTarget()}
	installer := NewDockerInstaller(connector, nil)
	if _, err := installer.Start(context.Background(), "vm", DockerInstallRequest{Method: DockerConvenience, Confirmation: "vm"}); err == nil {
		t.Fatal("wrong confirmation accepted")
	}
	if _, err := installer.Start(context.Background(), "vm", DockerInstallRequest{Method: "curl|sh", Confirmation: "vm-1"}); err == nil {
		t.Fatal("unknown method accepted")
	}
	local := NewDockerInstaller(&installConnector{ex: host, target: Server{Name: "local", Kind: KindLocal}}, nil)
	if _, err := local.Start(context.Background(), "local", DockerInstallRequest{Method: DockerConvenience, Confirmation: "local"}); err == nil {
		t.Fatal("local server accepted")
	}
	if len(host.Commands) != 0 {
		t.Fatalf("refused request ran %v", host.Commands)
	}
	snapshot, err := installer.Start(context.Background(), "vm", DockerInstallRequest{Method: DockerConvenience, Confirmation: " vm-1 "})
	if err != nil || !snapshot.Running {
		t.Fatalf("%+v %v", snapshot, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for installer.Status("vm").Running && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	final := installer.Status("vm")
	if final.Running || final.Error != "" || !strings.Contains(strings.Join(final.Lines, "\n"), "27.3.1") {
		t.Fatalf("final %+v", final)
	}
}
