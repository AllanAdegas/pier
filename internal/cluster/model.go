// Package cluster is the optional Kubernetes context. A cluster is either
// imported (any conformant cluster, reached with a kubeconfig) or a
// single-node K3s that Pier runs as a container on one of its servers.
// Nothing else in Pier depends on this package: Kubernetes is opt-in.
package cluster

import (
	"regexp"
	"strings"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/server"
)

// Source tells where a cluster came from.
type Source string

const (
	SourceImported Source = "imported"
	SourceK3s      Source = "k3s"
)

// Status is the result of the last health check.
type Status string

const (
	StatusUnknown      Status = "unknown"
	StatusProvisioning Status = "provisioning"
	StatusReady        Status = "ready"
	StatusUnreachable  Status = "unreachable"
)

// DefaultK3sImage is the K3s release Pier is tested against.
const DefaultK3sImage = "rancher/k3s:v1.36.5-k3s1"

// K3sConfig configures a K3s cluster running as a container.
type K3sConfig struct {
	Image string `json:"image"`
	// APIPort publishes the Kubernetes API on the server.
	APIPort int `json:"api_port"`
	// HTTPPort publishes the cluster's ingress controller (port 80 of the node).
	HTTPPort int `json:"http_port"`
}

// Cluster is a Kubernetes cluster Pier can operate.
type Cluster struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Source        Source     `json:"source"`
	ServerID      string     `json:"server_id"`
	K3s           K3sConfig  `json:"k3s"`
	Endpoint      string     `json:"endpoint"`
	Status        Status     `json:"status"`
	StatusMessage string     `json:"status_message"`
	Version       string     `json:"version"`
	Nodes         int        `json:"nodes"`
	LastCheckedAt *time.Time `json:"last_checked_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ImportInput carries what is needed to register an existing cluster.
type ImportInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Kubeconfig  string `json:"kubeconfig"`
	// Context selects one context of the kubeconfig; empty uses the current one.
	Context string `json:"context"`
}

// K3sInput carries what is needed to create a K3s cluster on a server.
type K3sInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	ServerID    string `json:"server_id"`
	APIPort     int    `json:"api_port"`
	HTTPPort    int    `json:"http_port"`
	Image       string `json:"image"`
}

const (
	defaultAPIPort  = 6443
	defaultHTTPPort = 8080
	kubeconfigPath  = "/etc/rancher/k3s/k3s.yaml"
)

var k3sImagePattern = regexp.MustCompile(`^rancher/k3s:[A-Za-z0-9._-]+$`)

func (i *K3sInput) validate() error {
	fields := apperr.Fields{}
	i.Name = strings.TrimSpace(i.Name)
	i.Image = strings.TrimSpace(i.Image)
	if i.Image == "" {
		i.Image = DefaultK3sImage
	}
	if i.APIPort == 0 {
		i.APIPort = defaultAPIPort
	}
	if i.HTTPPort == 0 {
		i.HTTPPort = defaultHTTPPort
	}
	if i.Name == "" {
		fields.Add("name", "Informe o nome do cluster.")
	}
	if i.ServerID == "" {
		fields.Add("server_id", "Selecione o servidor.")
	}
	if !k3sImagePattern.MatchString(i.Image) {
		fields.Add("image", "Use uma imagem oficial do K3s, ex.: %s.", DefaultK3sImage)
	}
	if i.APIPort < 1024 || i.APIPort > 65535 {
		fields.Add("api_port", "Use uma porta entre 1024 e 65535.")
	}
	if i.HTTPPort < 1024 || i.HTTPPort > 65535 || i.HTTPPort == i.APIPort {
		fields.Add("http_port", "Use uma porta entre 1024 e 65535, diferente da porta da API.")
	}
	return fields.Err()
}

// nodeAddress is how the API of a K3s cluster is reached from Pier.
func (s *Service) nodeAddress(target server.Server) string {
	if target.Kind == server.KindLocal {
		return s.localAddress
	}
	return target.Host
}

// ContainerName is the K3s container of a created cluster. The node name
// inside K3s keeps the ID: renaming a node would make it a new node.
func (c Cluster) ContainerName() string { return docker.ReadableName(c.Name, c.ID) }

// k3sCompose renders the compose project that runs a single-node K3s.
//
// The node needs a privileged container (it runs containerd and the kubelet
// inside). Its state lives in a named volume, so recreating the container
// keeps the cluster.
func k3sCompose(cluster Cluster, address string) *compose.File {
	volume := cluster.ID + "_data"
	return &compose.File{
		Services: map[string]*compose.Service{
			"k3s": {
				Image:         cluster.K3s.Image,
				ContainerName: cluster.ContainerName(),
				Restart:       "unless-stopped",
				Privileged:    true,
				// The address is added to the API certificate so clients
				// outside the server can verify it.
				Command: []string{"server", "--tls-san", address, "--node-name", "pier-" + cluster.ID},
				Environment: map[string]string{
					"K3S_KUBECONFIG_OUTPUT": kubeconfigPath,
					"K3S_KUBECONFIG_MODE":   "600",
				},
				Labels:  docker.Labels(docker.TypeCluster, cluster.ID),
				Tmpfs:   []string{"/run", "/var/run"},
				Volumes: []string{volume + ":/var/lib/rancher/k3s"},
				Ports: []string{
					portMapping(cluster.K3s.APIPort, 6443),
					portMapping(cluster.K3s.HTTPPort, 80),
				},
			},
		},
		Volumes: map[string]*compose.Volume{volume: {Name: volume}},
	}
}
