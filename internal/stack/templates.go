package stack

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"

	"pier/internal/platform/apperr"
)

// The catalog: one YAML file per service in templates/, in Pier's format
// (see magic.go for the variables Pier fills in).
//
//go:embed templates/*.yaml
var templateFiles embed.FS

var loadTemplates = sync.OnceValues(func() ([]Template, error) {
	paths, err := fs.Glob(templateFiles, "templates/*.yaml")
	if err != nil {
		return nil, err
	}
	templates := make([]Template, 0, len(paths))
	for _, file := range paths {
		template, err := loadTemplate(file)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	sort.Slice(templates, func(i, j int) bool {
		return strings.ToLower(templates[i].Name) < strings.ToLower(templates[j].Name)
	})
	return templates, nil
})

func loadTemplate(file string) (Template, error) {
	raw, err := templateFiles.ReadFile(file)
	if err != nil {
		return Template{}, err
	}
	var template Template
	if err := yaml.Unmarshal(raw, &template); err != nil {
		return Template{}, fmt.Errorf("template %s: %w", file, err)
	}
	template.Slug = strings.TrimSuffix(path.Base(file), ".yaml")
	if err := expandBlueprint(&template); err != nil {
		return Template{}, fmt.Errorf("template %s: %w", file, err)
	}
	doc, err := parseCompose(template.Compose)
	if err != nil {
		return Template{}, fmt.Errorf("template %s: %w", file, err)
	}
	if template.Routes == nil {
		template.Routes = []TemplateRoute{}
	}
	if template.Tags == nil {
		template.Tags = []string{}
	}
	template.Variables = userVariables(doc)
	template.Risks = doc.risks()
	template.HostPorts = doc.hostPorts()
	switch template.Auth {
	case "first-visitor":
		template.Risks = append(template.Risks, "first-visitor-setup")
	case "none":
		template.Risks = append(template.Risks, "no-auth")
	case "", "generated":
	default:
		return Template{}, fmt.Errorf("template %s: invalid auth metadata", file)
	}
	return template, nil
}

// Risks a template may carry: each gives its containers power over the
// server, beyond the stack itself.
const (
	RiskDockerSocket = "docker-socket"
	RiskPrivileged   = "privileged"
	RiskHostNetwork  = "host-network"
	RiskHostPID      = "host-pid"
)

// risks lists, in a stable order, what the compose file asks for that
// reaches beyond its containers.
func (d document) risks() []string {
	found := map[string]bool{}
	services, _ := d["services"].(map[string]any)
	for _, definition := range services {
		service, _ := definition.(map[string]any)
		if privileged, _ := service["privileged"].(bool); privileged {
			found[RiskPrivileged] = true
		}
		if mode, _ := service["network_mode"].(string); mode == "host" {
			found[RiskHostNetwork] = true
		}
		if pid, _ := service["pid"].(string); pid == "host" {
			found[RiskHostPID] = true
		}
		if ports, _ := service["ports"].([]any); len(ports) > 0 {
			found["host-ports"] = true
		}
		if devices, _ := service["devices"].([]any); len(devices) > 0 {
			found["capabilities"] = true
		}
		for _, cap := range stringList(service["cap_add"]) {
			switch strings.ToUpper(cap) {
			case "ALL", "SYS_ADMIN", "SYS_MODULE", "SYS_PTRACE", "SYS_TIME", "NET_ADMIN", "DAC_READ_SEARCH":
				found["capabilities"] = true
			}
		}
		for _, opt := range stringList(service["security_opt"]) {
			if opt == "seccomp:unconfined" || opt == "apparmor:unconfined" || opt == "seccomp=unconfined" || opt == "apparmor=unconfined" {
				found["unconfined"] = true
			}
		}
		for _, key := range []string{"ipc", "userns_mode", "cgroup"} {
			if service[key] == "host" {
				found["unconfined"] = true
			}
		}
		volumes, _ := service["volumes"].([]any)
		for _, volume := range volumes {
			source, target, ro := bindParts(volume)
			source = hostBindSource(source)
			if source == "/var/run/docker.sock" || source == "/run/docker.sock" {
				found[RiskDockerSocket] = true
			}
			if strings.HasPrefix(source, "/") && !allowedHostBind(source, target, ro) {
				found["host-path"] = true
			}
		}
	}
	risks := []string{}
	for _, risk := range []string{RiskDockerSocket, RiskPrivileged, RiskHostNetwork, RiskHostPID, "host-ports", "capabilities", "unconfined", "host-path"} {
		if found[risk] {
			risks = append(risks, risk)
		}
	}
	return risks
}

// userVariables keeps the variables a person may want to set when creating a
// stack from a template: the ones Pier neither generates nor derives.
func userVariables(doc document) []Variable {
	variables := []Variable{}
	for _, variable := range doc.referencedVariables() {
		if variable.Kind == VariableUser {
			variables = append(variables, variable)
		}
	}
	return variables
}

// CheckTemplate verifies that a template can become a runnable stack: its
// compose file parses, its routes point to services that exist and the
// result renders. The tests rely on it.
func CheckTemplate(template Template) error {
	routes := []Route{}
	for _, route := range template.Routes {
		routes = append(routes, Route{Service: route.Service, Port: route.Port, Domain: "https://check.example.com" + route.Path})
	}
	candidate := Stack{ID: "check", Name: template.Name, Compose: template.Compose, Routes: routes}
	if err := validate(&candidate, template.Compose, true, apperr.Fields{}); err != nil {
		return err
	}
	_, _, err := render(candidate)
	return err
}

// Templates returns the catalog sorted by name.
func Templates() ([]Template, error) { return loadTemplates() }

// FindTemplate looks a template up by slug.
func FindTemplate(slug string) (Template, bool) {
	templates, err := loadTemplates()
	if err != nil {
		return Template{}, false
	}
	for _, template := range templates {
		if template.Slug == slug {
			return template, true
		}
	}
	return Template{}, false
}
