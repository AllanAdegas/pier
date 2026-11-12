package application

import (
	"context"
	"strings"
	"testing"

	"pier/internal/compose"
)

func dockerOptionsConfig(options DockerOptions) Config {
	config := imageConfig()
	config.DockerOptions = options
	return config
}

func TestDockerOptionsAcceptsSafeAllowlist(t *testing.T) {
	config := dockerOptionsConfig(DockerOptions{
		ExtraHosts: []string{"api.internal:10.0.0.5", "host.docker.internal=host-gateway", "v6:::1"},
		CapAdd:     []string{"cap_net_admin", "SYS_NICE"}, CapDrop: []string{"ALL"},
		Ulimits: []Ulimit{{Name: "NOFILE", Soft: 1024, Hard: 4096}, {Name: "memlock", Soft: -1, Hard: -1}},
		ShmSize: "256m", Labels: map[string]string{"com.example.team": "web"},
	})
	if err := config.Validate(); err != nil {
		t.Fatalf("safe options refused: %v", err)
	}
	if got := config.DockerOptions.ExtraHosts[1]; got != "host.docker.internal:host-gateway" {
		t.Errorf("extra host not normalized: %s", got)
	}
	if config.DockerOptions.CapAdd[0] != "NET_ADMIN" || config.DockerOptions.Ulimits[0].Name != "nofile" {
		t.Errorf("not normalized: %+v", config.DockerOptions)
	}
	if found := config.DangerousDockerOptions(); len(found) != 0 {
		t.Errorf("safe options reported dangerous: %v", found)
	}
	if err := dockerOptionsAllowed(config, DockerOptionsPolicy{}); err != nil {
		t.Errorf("safe options need no policy: %v", err)
	}
}

func TestDockerOptionsRejectsInvalidFormats(t *testing.T) {
	cases := map[string]DockerOptions{
		"docker_options.extra_hosts":  {ExtraHosts: []string{"bad host:1.2.3.4"}},
		"docker_options.cap_add":      {CapAdd: []string{"NOT_A_CAP"}},
		"docker_options.cap_drop":     {CapDrop: []string{"WHATEVER"}},
		"docker_options.ulimits":      {Ulimits: []Ulimit{{Name: "nofile", Soft: 10, Hard: 5}}},
		"docker_options.shm_size":     {ShmSize: "lots"},
		"docker_options.labels":       {Labels: map[string]string{"traefik.http.routers.x.rule": "Host(`evil`)"}},
		"docker_options.devices":      {Devices: []string{"/etc/passwd:/dev/x"}},
		"docker_options.network_mode": {NetworkMode: "container:other"},
		"docker_options.pid_mode":     {PidMode: "container:other"},
	}
	for field, options := range cases {
		config := dockerOptionsConfig(options)
		if _, found := fieldsOf(t, config.Validate())[field]; !found {
			t.Errorf("%s: expected a validation error for %+v", field, options)
		}
	}
	for _, label := range []string{"pier.id", "PIER.managed", "com.docker.compose.project"} {
		config := dockerOptionsConfig(DockerOptions{Labels: map[string]string{label: "x"}})
		if _, found := fieldsOf(t, config.Validate())["docker_options.labels"]; !found {
			t.Errorf("reserved label %s accepted", label)
		}
	}
	hostWithDomain := dockerOptionsConfig(DockerOptions{NetworkMode: "host"})
	if _, found := fieldsOf(t, hostWithDomain.Validate())["docker_options.network_mode"]; !found {
		t.Error("host network with a domain must be refused")
	}
}

// Every dangerous option is refused without the instance switch and accepted
// with it.
func TestDockerOptionsEveryDangerousOptionNeedsTheOwnerSwitch(t *testing.T) {
	cases := map[string]Config{
		"privileged":         dockerOptionsConfig(DockerOptions{Privileged: true}),
		"network_mode: host": hostNetworkConfig(),
		"pid: host":          dockerOptionsConfig(DockerOptions{PidMode: "host"}),
	}
	for _, capability := range dangerousCapabilities {
		cases["cap_add: "+capability] = dockerOptionsConfig(DockerOptions{CapAdd: []string{capability}})
	}
	for _, device := range []string{"/dev/mem", "/dev/kmem", "/dev/port", "/dev/sda", "/dev/nvme0n1", "/dev/mapper/root", "/dev/loop0"} {
		config := dockerOptionsConfig(DockerOptions{Devices: []string{device + ":/dev/x:rw"}})
		cases["device: "+device] = config
	}
	for _, source := range []string{"/var/run/docker.sock", "/run/docker.sock", "/var/run", "/run", "/", "/var", "/var/run/../run/docker.sock", "/home/u/.docker/run/docker.sock"} {
		config := dockerOptionsConfig(DockerOptions{})
		config.Storages = []Storage{{Kind: StorageBind, Source: source, Target: "/var/run/docker.sock"}}
		cases["volume: "+source] = config
	}
	for name, config := range cases {
		if err := config.Validate(); err != nil {
			t.Errorf("%s: format should be valid: %v", name, err)
			continue
		}
		if found := config.DangerousDockerOptions(); len(found) == 0 {
			t.Errorf("%s: not detected as dangerous", name)
		}
		err := dockerOptionsAllowed(config, DockerOptionsPolicy{AllowDevices: true})
		if _, found := fieldsOf(t, err)["docker_options"]; !found {
			t.Errorf("%s: accepted without the dangerous-options switch", name)
		}
		if err := dockerOptionsAllowed(config, DockerOptionsPolicy{AllowDangerous: true, AllowDevices: true}); err != nil {
			t.Errorf("%s: refused with the switch on: %v", name, err)
		}
	}
}

func hostNetworkConfig() Config {
	config := dockerOptionsConfig(DockerOptions{NetworkMode: "host"})
	config.Network.Domains = []string{}
	return config
}

func TestDockerOptionsDevicesNeedTheDevicesSwitch(t *testing.T) {
	config := dockerOptionsConfig(DockerOptions{Devices: []string{"/dev/ttyUSB0"}})
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, found := fieldsOf(t, dockerOptionsAllowed(config, DockerOptionsPolicy{AllowDangerous: true}))["docker_options.devices"]; !found {
		t.Fatal("devices accepted while the instance does not allow them")
	}
	if err := dockerOptionsAllowed(config, DockerOptionsPolicy{AllowDevices: true}); err != nil {
		t.Fatalf("an ordinary device needs only the devices switch: %v", err)
	}
}

func TestDockerOptionsPolicyIsEnforcedOnSaveAndDeploy(t *testing.T) {
	fixture := newDeployFixture(t, imageConfig())
	service := NewService(fixture.store, fixture.env, fakeConnector{fixture.ex}, nil)
	ctx := context.Background()
	privileged := dockerOptionsConfig(DockerOptions{Privileged: true})
	if _, err := service.Create(ctx, CreateInput{EnvironmentID: "env1", ServerID: "srv1", Name: "p", Config: privileged}); err == nil {
		t.Fatal("privileged application created without the owner switch")
	}
	if _, err := service.SetDockerOptionsPolicy(ctx, DockerOptionsPolicy{AllowDangerous: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Update(ctx, fixture.app.ID, UpdateInput{Name: "demo", Config: privileged}); err != nil {
		t.Fatalf("allowed configuration refused: %v", err)
	}
	if _, err := service.SetDockerOptionsPolicy(ctx, DockerOptionsPolicy{}); err != nil {
		t.Fatal(err)
	}
	deployment := fixture.deploy(t)
	if deployment.Status != DeploymentFailed || !strings.Contains(deployment.Error, "privileged") {
		t.Fatalf("deploy must fail closed after the switch is turned off: %s %s", deployment.Status, deployment.Error)
	}
	if fixture.ex.Ran("docker pull") {
		t.Fatal("the image was pulled before the policy check")
	}
}

func TestDockerOptionsAreAppliedToEveryReplica(t *testing.T) {
	config := dockerOptionsConfig(DockerOptions{
		ExtraHosts: []string{"api:10.0.0.5"}, CapAdd: []string{"NET_ADMIN"}, CapDrop: []string{"MKNOD"},
		Ulimits: []Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}}, ShmSize: "1g", Devices: []string{"/dev/ttyUSB0"},
		Labels: map[string]string{"team": "web", "pier.id": "spoof"},
	})
	config.Processes = []Process{{Name: "web", Replicas: 2}, {Name: "worker", Command: "run", Replicas: 1}}
	file, err := composeFor(releaseSpec{App: Application{ID: "app", Config: config}, Image: "img", ContainerName: "app-1", DeploymentID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Services) != 3 {
		t.Fatalf("services = %d", len(file.Services))
	}
	for name, service := range file.Services {
		if service.ShmSize != "1g" || service.CapAdd[0] != "NET_ADMIN" || service.CapDrop[0] != "MKNOD" ||
			service.Ulimits["nofile"] != (compose.Ulimit{Soft: 1024, Hard: 2048}) || service.Devices[0] != "/dev/ttyUSB0" ||
			service.ExtraHosts[0] != "api:10.0.0.5" || service.Labels["team"] != "web" {
			t.Errorf("%s: options not applied: %+v", name, service)
		}
		if service.Labels["pier.id"] != "app" {
			t.Errorf("%s: a user label overrode Pier's: %s", name, service.Labels["pier.id"])
		}
	}
	yaml, err := file.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"cap_add:", "ulimits:", "nofile:", "soft: 1024", "shm_size: 1g", "devices:"} {
		if !strings.Contains(string(yaml), expected) {
			t.Errorf("compose lacks %q", expected)
		}
	}
}

func TestDockerOptionsHostNetworkDropsPierNetwork(t *testing.T) {
	config := hostNetworkConfig()
	config.DockerOptions.PidMode = "host"
	file, err := composeFor(releaseSpec{App: Application{ID: "app", Config: config}, Image: "img", ContainerName: "app-1", DeploymentID: "d"})
	if err != nil {
		t.Fatal(err)
	}
	service := file.Services["app-1"]
	if service.NetworkMode != "host" || service.Networks != nil || service.Pid != "host" {
		t.Fatalf("host namespaces not rendered: %+v", service)
	}
}
