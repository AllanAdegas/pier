// Package engine is the "plain Docker" context: it works directly with the
// containers, images, volumes and networks of a server, whether or not Pier
// created them. Docker itself is the source of truth; nothing here is stored.
package engine

import (
	"regexp"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
)

// LabelStandalone marks containers started through the plain Docker screen.
const LabelStandalone = "pier.standalone"

// Container is one container of a server, as `docker ps -a` reports it.
type Container struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Image    string `json:"image"`
	Command  string `json:"command"`
	State    string `json:"state"`
	Status   string `json:"status"`
	Ports    string `json:"ports"`
	Networks string `json:"networks"`
	Created  string `json:"created"`
	// Managed is true when Pier owns the container through an application,
	// database, stack, proxy or cluster; Owner tells which one.
	Managed bool   `json:"managed"`
	Owner   *Owner `json:"owner"`
	// ComposeProject is set for containers started by any docker compose.
	ComposeProject string `json:"compose_project"`
}

// Owner points to the Pier resource that manages a container.
type Owner struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// Image is one image stored on a server.
type Image struct {
	ID         string `json:"id"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
	Size       string `json:"size"`
	Created    string `json:"created"`
}

// Reference is how the image is addressed: "repo:tag", or the id when untagged.
func (i Image) Reference() string {
	if i.Repository == "" || i.Repository == "<none>" || i.Tag == "<none>" {
		return i.ID
	}
	return i.Repository + ":" + i.Tag
}

// Volume is one named volume of a server.
type Volume struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
}

// Network is one docker network of a server.
type Network struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Scope  string `json:"scope"`
	// Reserved is true for networks Pier or Docker depend on.
	Reserved bool `json:"reserved"`
}

// RunSpec describes a container to start with `docker run`.
type RunSpec struct {
	Name    string   `json:"name"`
	Image   string   `json:"image"`
	Command string   `json:"command"`
	Env     []string `json:"env"`
	Ports   []string `json:"ports"`
	Volumes []string `json:"volumes"`
	Network string   `json:"network"`
	Restart string   `json:"restart"`
}

var (
	envPattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=[^\n\x00]*$`)
	portMapPattern = regexp.MustCompile(`^((\d{1,3}\.){3}\d{1,3}:)?\d{1,5}:\d{1,5}(/(tcp|udp))?$`)
	// source:target[:ro|rw], where source is a volume name or an absolute path.
	mountPattern = regexp.MustCompile(`^(/[^:\n\x00]*|[a-zA-Z0-9][a-zA-Z0-9_.-]*):/[^:\n\x00]+(:(ro|rw))?$`)
)

var restartPolicies = []string{"no", "always", "unless-stopped", "on-failure"}

func (s *RunSpec) validate() error {
	fields := apperr.Fields{}
	s.Name = strings.TrimSpace(s.Name)
	s.Image = strings.TrimSpace(s.Image)
	s.Command = strings.TrimSpace(s.Command)
	s.Network = strings.TrimSpace(s.Network)
	s.Env, s.Ports, s.Volumes = compact(s.Env), compact(s.Ports), compact(s.Volumes)
	if s.Restart == "" {
		s.Restart = "unless-stopped"
	}
	if !docker.ValidObjectName(s.Name) {
		fields.Add("name", "Use letras, números, '-', '_' e '.', começando por letra ou número.")
	}
	if !docker.ValidImage(s.Image) {
		fields.Add("image", "Informe uma imagem válida, ex.: nginx:1.27.")
	}
	if s.Network != "" && !docker.ValidObjectName(s.Network) {
		fields.Add("network", "Nome de rede inválido.")
	}
	if !contains(restartPolicies, s.Restart) {
		fields.Add("restart", "Política de reinício desconhecida.")
	}
	for _, entry := range s.Env {
		if !envPattern.MatchString(entry) {
			fields.Add("env", "%q: use o formato CHAVE=valor.", entry)
		}
	}
	for _, mapping := range s.Ports {
		if !portMapPattern.MatchString(mapping) {
			fields.Add("ports", "%q: use host:container, ex.: 8080:80.", mapping)
		}
	}
	for _, mount := range s.Volumes {
		if !mountPattern.MatchString(mount) {
			fields.Add("volumes", "%q: use volume:/caminho ou /host:/caminho.", mount)
		}
	}
	return fields.Err()
}

func compact(values []string) []string {
	out := []string{}
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
