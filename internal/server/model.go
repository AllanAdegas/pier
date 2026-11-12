// Package server is the bounded context for the machines Pier deploys to:
// servers, the SSH keys that reach them and the reverse proxy running on them.
package server

import (
	"path"
	"time"

	"pier/internal/compose"
)

// Kind tells how Pier reaches a server.
type Kind string

const (
	KindLocal Kind = "local"
	KindSSH   Kind = "ssh"
)

// Status is the result of the last reachability check.
type Status string

const (
	StatusUnknown     Status = "unknown"
	StatusReachable   Status = "reachable"
	StatusUnreachable Status = "unreachable"
)

// ProxySettings configures the Traefik instance of a server.
type ProxySettings struct {
	HTTPPort  int       `json:"http_port"`
	HTTPSPort int       `json:"https_port"`
	ACMEEmail string    `json:"acme_email"`
	Edge      EdgeProxy `json:"edge"`
}

// Entry tells the publications of the server how browsers reach its proxy.
func (p ProxySettings) Entry() compose.ProxyEntry {
	return compose.ProxyEntry{HTTPSPort: p.HTTPSPort, Edge: p.Edge.Enabled()}
}

// EdgeProxy is a Caddy that already owns ports 80/443 of the server (it was
// there before Pier). Pier's proxy runs on other ports behind it, and Pier
// keeps a block of the Caddyfile forwarding Pier's domains: Caddy terminates
// TLS with its own certificates and passes plain HTTP to Pier's proxy.
type EdgeProxy struct {
	// Container is the name of the Caddy container.
	Container string `json:"container"`
	// Caddyfile is the path of the Caddyfile inside the container; it must be
	// bind-mounted for durable edits. Pier accesses it inside the container.
	Caddyfile string `json:"caddyfile"`
}

// DefaultCaddyfile is where the official Caddy image reads its configuration.
const DefaultCaddyfile = "/etc/caddy/Caddyfile"

// Enabled reports whether the server has an edge proxy.
func (e EdgeProxy) Enabled() bool { return e.Container != "" }

// DefaultProxySettings binds the standard web ports.
func DefaultProxySettings() ProxySettings {
	return ProxySettings{HTTPPort: 80, HTTPSPort: 443}
}

// Server is a machine with Docker where resources run.
type Server struct {
	ID            string        `json:"id"`
	ProxyToken    string        `json:"-"`
	EdgeTrustedIP string        `json:"-"`
	Name          string        `json:"name"`
	Description   string        `json:"description"`
	Kind          Kind          `json:"kind"`
	Host          string        `json:"host"`
	Port          int           `json:"port"`
	User          string        `json:"user"`
	PrivateKeyID  string        `json:"private_key_id"`
	HostKey       string        `json:"host_key"`
	DataDir       string        `json:"data_dir"`
	Proxy         ProxySettings `json:"proxy"`
	Status        Status        `json:"status"`
	Checks        []Check       `json:"checks"`
	StatusMessage string        `json:"status_message"`
	DockerVersion string        `json:"docker_version"`
	LastCheckedAt *time.Time    `json:"last_checked_at"`
	CreatedAt     time.Time     `json:"created_at"`
}

// ResourceDir is where the compose project of a resource lives on the server.
func (s Server) ResourceDir(resourceType, resourceID string) string {
	return path.Join(s.DataDir, resourceType+"s", resourceID)
}

// BuildDir is the scratch directory of one deployment.
func (s Server) BuildDir(deploymentID string) string {
	return path.Join(s.DataDir, "builds", deploymentID)
}

// BackupDir is where database dumps are kept.
func (s Server) BackupDir(databaseID string) string {
	return path.Join(s.DataDir, "backups", databaseID)
}

// ProxyDir holds the Traefik compose project, certificates and dynamic config.
func (s Server) ProxyDir() string { return path.Join(s.DataDir, "proxy") }

// PrivateKey is an SSH key used to reach servers or private Git repositories.
// The private half never leaves the backend.
type PrivateKey struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	PublicKey   string    `json:"public_key"`
	Fingerprint string    `json:"fingerprint"`
	CreatedAt   time.Time `json:"created_at"`
}

// Input carries the editable fields of a server.
type Input struct {
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Host         string        `json:"host"`
	Port         int           `json:"port"`
	User         string        `json:"user"`
	PrivateKeyID string        `json:"private_key_id"`
	DataDir      string        `json:"data_dir"`
	Proxy        ProxySettings `json:"proxy"`
}

// Metrics is a point-in-time snapshot of a server's capacity and usage.
type Metrics struct {
	CollectedAt          time.Time        `json:"collected_at"`
	CPUPercent           *float64         `json:"cpu_percent"`
	MemoryUsedBytes      *int64           `json:"memory_used_bytes"`
	MemoryAvailableBytes *int64           `json:"memory_available_bytes"`
	SwapTotalBytes       *int64           `json:"swap_total_bytes"`
	SwapUsedBytes        *int64           `json:"swap_used_bytes"`
	UptimeSeconds        *float64         `json:"uptime_seconds"`
	LoadAverage          []float64        `json:"load_average"`
	DiskAvailableBytes   *int64           `json:"disk_available_bytes"`
	ContainersTotal      int              `json:"containers_total"`
	ContainersStopped    int              `json:"containers_stopped"`
	ContainersPaused     int              `json:"containers_paused"`
	HostError            string           `json:"host_error"`
	DiskError            string           `json:"disk_error"`
	OperatingSystem      string           `json:"operating_system"`
	Architecture         string           `json:"architecture"`
	CPUs                 int              `json:"cpus"`
	MemoryTotalBytes     int64            `json:"memory_total_bytes"`
	DiskTotalBytes       int64            `json:"disk_total_bytes"`
	DiskUsedBytes        int64            `json:"disk_used_bytes"`
	ContainersRunning    int              `json:"containers_running"`
	Images               int              `json:"images"`
	Containers           []ContainerUsage `json:"containers"`
}

// ContainerUsage is the live resource usage of one managed container.
type ContainerUsage struct {
	State         string `json:"state"`
	Status        string `json:"status"`
	Name          string `json:"name"`
	ResourceType  string `json:"resource_type"`
	ResourceID    string `json:"resource_id"`
	CPUPercent    string `json:"cpu_percent"`
	MemoryUsage   string `json:"memory_usage"`
	MemoryPercent string `json:"memory_percent"`
}

// ProxyState describes the proxy container of a server.
type ProxyState struct {
	Running bool   `json:"running"`
	Status  string `json:"status"`
	Image   string `json:"image"`
}
