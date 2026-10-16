// Package stack is the bounded context for multi-container services defined
// by a docker compose file, written by the user or taken from the catalog.
package stack

import (
	"time"

	"pier/internal/compose"
)

// Route publishes one service of the stack on a domain through the proxy.
type Route struct {
	Service string `json:"service"`
	Port    int    `json:"port"`
	Domain  string `json:"domain"`
}

// Stack is a compose project managed by Pier.
type Stack struct {
	ID            string `json:"id"`
	EnvironmentID string `json:"environment_id"`
	ServerID      string `json:"server_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Template      string `json:"template"`
	// Compose is the compose file kept by Pier. It is empty for a stack that
	// runs from a directory: there the file on the server is the source.
	Compose string `json:"compose"`
	// Directory, when set, is a folder of the server holding the compose file
	// the stack runs. Relative paths, env files and secret files of that
	// compose file are read from the folder, where they stay.
	Directory string `json:"directory"`
	// ComposeFile is the compose file inside Directory.
	ComposeFile string `json:"compose_file"`
	// ProjectName is the docker compose project name. It defaults to the id;
	// a directory stack may reuse the name of a project that already runs
	// from that folder, to keep its volumes.
	ProjectName string `json:"project_name"`
	// ComposeProject is the project name given at creation when ProjectName
	// is empty: the stack's name made readable, plus the start of its id.
	ComposeProject string  `json:"compose_project"`
	Routes         []Route `json:"routes"`
	// proxyEntry is how browsers reach the proxy of the stack's server; set
	// when the server is reached, never stored.
	proxyEntry compose.ProxyEntry
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// FromDirectory reports whether the stack runs a compose file that lives in
// a folder of the server.
func (s Stack) FromDirectory() bool { return s.Directory != "" }

// CreateInput carries what is needed to register a stack. One source must be
// given: Template (a catalog slug), Compose (the file itself) or Directory
// (a folder of the server that holds the file).
type CreateInput struct {
	EnvironmentID string  `json:"environment_id"`
	ServerID      string  `json:"server_id"`
	Name          string  `json:"name"`
	Description   string  `json:"description"`
	Template      string  `json:"template"`
	Compose       string  `json:"compose"`
	Directory     string  `json:"directory"`
	ComposeFile   string  `json:"compose_file"`
	ProjectName   string  `json:"project_name"`
	Routes        []Route `json:"routes"`
	// Env holds variables to define right away (the ones a template requires).
	Env map[string]string `json:"env"`
}

// UpdateInput carries the editable fields of a stack.
type UpdateInput struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Compose     string  `json:"compose"`
	Directory   string  `json:"directory"`
	ComposeFile string  `json:"compose_file"`
	Routes      []Route `json:"routes"`
}

// TemplateRoute tells which service of a template is meant to be published.
type TemplateRoute struct {
	Service string `yaml:"service" json:"service"`
	Port    int    `yaml:"port" json:"port"`
	// Path is a prefix this service answers under, when several services of
	// the template share one domain.
	Path string `yaml:"path,omitempty" json:"path"`
}

// Template is a ready-to-run compose definition from the catalog.
type Template struct {
	Slug        string          `yaml:"-" json:"slug"`
	Name        string          `yaml:"name" json:"name"`
	Description string          `yaml:"description" json:"description"`
	Category    string          `yaml:"category" json:"category"`
	Website     string          `yaml:"website" json:"website"`
	Tags        []string        `yaml:"tags,omitempty" json:"tags"`
	Routes      []TemplateRoute `yaml:"routes" json:"routes"`
	Compose     string          `yaml:"compose" json:"compose,omitempty"`
	// Verified templates were deployed and checked by hand; the others are
	// only known to be valid compose files.
	Verified bool `yaml:"verified,omitempty" json:"verified"`
	// Variables are the ones the compose file interpolates; computed on load.
	Variables []Variable `yaml:"-" json:"variables,omitempty"`
	// Risks are the powers over the server the template asks for; computed
	// on load so the creation form can warn before anything runs.
	Risks []string `yaml:"-" json:"risks"`
}

// Summary is the template without its compose file, for listings.
func (t Template) Summary() Template {
	t.Compose = ""
	t.Variables = nil
	return t
}

// composeProject is the docker compose project the stack runs as: the name
// chosen for a directory stack, the readable name given at creation, or the
// id for stacks created before readable names. It never changes, because
// volumes and networks are named after it.
func (s Stack) composeProject() string {
	switch {
	case s.ProjectName != "":
		return s.ProjectName
	case s.ComposeProject != "":
		return s.ComposeProject
	}
	return s.ID
}
