// Package swarm is the Docker Swarm context: clusters Pier creates from its
// registered servers (init on a manager, join of workers and managers),
// their nodes and the destructive operations on them (drain, remove, leave).
// Join tokens are read from a manager when a node joins, travel on stdin and
// are never stored or logged. Deploying applications to a swarm belongs to
// the application context, which reaches the managers through a port.
package swarm

import (
	"net/netip"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
)

// Cluster is a swarm created by Pier.
type Cluster struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	SwarmID       string    `json:"swarm_id"`
	AdvertiseAddr string    `json:"advertise_addr"`
	Nodes         []Node    `json:"nodes"`
	CreatedAt     time.Time `json:"created_at"`
}

// Node is a registered server that belongs to a cluster.
type Node struct {
	ServerID      string    `json:"server_id"`
	ServerName    string    `json:"server_name"`
	NodeID        string    `json:"node_id"`
	Role          string    `json:"role"`
	Primary       bool      `json:"primary"`
	AdvertiseAddr string    `json:"advertise_addr"`
	JoinedAt      time.Time `json:"joined_at"`
}

// NodeState is a node as the panel shows it: the record and what a manager
// reports about it now (empty Live when the manager could not be reached or
// does not know the node).
type NodeState struct {
	Node
	Live *docker.SwarmNode `json:"live"`
}

// Overview is a cluster with the live state of its nodes.
type Overview struct {
	Cluster
	Reachable bool        `json:"reachable"`
	Message   string      `json:"message"`
	States    []NodeState `json:"states"`
	// Unmanaged are nodes of the swarm that are not Pier servers.
	Unmanaged []docker.SwarmNode `json:"unmanaged"`
}

// CreateInput initializes a swarm on a manager.
type CreateInput struct {
	Name            string `json:"name"`
	Description     string `json:"description"`
	ManagerServerID string `json:"manager_server_id"`
	// AdvertiseAddr is the IP the other nodes reach the manager at.
	AdvertiseAddr string `json:"advertise_addr"`
}

// JoinInput adds a registered server to a swarm.
type JoinInput struct {
	ServerID string `json:"server_id"`
	Role     string `json:"role"`
	// AdvertiseAddr is the IP the other nodes reach this node at; empty uses
	// the server's host when it is an IP, else Docker's own choice.
	AdvertiseAddr string `json:"advertise_addr"`
}

// Confirmation carries the name typed to confirm a destructive step.
type Confirmation struct {
	Confirmation string `json:"confirmation"`
}

const (
	maxNameLength   = 60
	maxDescription  = 500
	minDockerMajor  = 24
	minDockerMinor  = 0
	maxClusterNodes = 50
)

func (i *CreateInput) validate() error {
	fields := apperr.Fields{}
	i.Name = strings.TrimSpace(i.Name)
	i.Description = strings.TrimSpace(i.Description)
	i.ManagerServerID = strings.TrimSpace(i.ManagerServerID)
	i.AdvertiseAddr = strings.TrimSpace(i.AdvertiseAddr)
	if i.Name == "" || len(i.Name) > maxNameLength {
		fields.Add("name", "Informe um nome de até %d caracteres.", maxNameLength)
	}
	if len(i.Description) > maxDescription {
		fields.Add("description", "Use no máximo %d caracteres.", maxDescription)
	}
	if i.ManagerServerID == "" {
		fields.Add("manager_server_id", "Selecione o servidor manager.")
	}
	if i.AdvertiseAddr != "" && !validAddress(i.AdvertiseAddr) {
		fields.Add("advertise_addr", "Use um endereço IP, como 10.0.0.5.")
	}
	return fields.Err()
}

func (i *JoinInput) validate() error {
	fields := apperr.Fields{}
	i.ServerID = strings.TrimSpace(i.ServerID)
	i.Role = strings.TrimSpace(i.Role)
	i.AdvertiseAddr = strings.TrimSpace(i.AdvertiseAddr)
	if i.ServerID == "" {
		fields.Add("server_id", "Selecione o servidor.")
	}
	if i.Role == "" {
		i.Role = docker.SwarmWorker
	}
	if i.Role != docker.SwarmWorker && i.Role != docker.SwarmManager {
		fields.Add("role", "Escolha manager ou worker.")
	}
	if i.AdvertiseAddr != "" && !validAddress(i.AdvertiseAddr) {
		fields.Add("advertise_addr", "Use um endereço IP, como 10.0.0.5.")
	}
	return fields.Err()
}

// validAddress accepts a plain IP address: it reaches docker as an argument
// and other nodes connect to it, so names and ports are refused.
func validAddress(value string) bool {
	address, err := netip.ParseAddr(value)
	return err == nil && !address.IsUnspecified() && !address.IsMulticast() && address.Zone() == ""
}

// hostAddress is the server host when it is an IP address.
func hostAddress(host string) string {
	if validAddress(host) {
		return host
	}
	return ""
}

func confirms(typed, expected string) bool {
	return expected != "" && strings.TrimSpace(typed) == expected
}
