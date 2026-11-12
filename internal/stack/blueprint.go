package stack

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// A blueprint is how catalog templates are written: the application's own
// services in a compact form, plus the components it needs ("with:"). On
// load it becomes the compose file the rest of Pier works with
// (Template.Compose), so validation, variables, risks and rendering are the
// same as for a compose file written by hand.
//
//	services:
//	  app:
//	    image: vendor/app:1.2.3
//	    port: 8080            # published through the proxy (a route)
//	    data: /var/lib/app    # a named volume "app-data" mounted there
//	    env:
//	      DATABASE_URL: "{{postgres.url}}"
//	      SECRET_KEY: ${PIER_SECRET_APP}
//	    health: wget:/health  # see healthTest
//	with:
//	  postgres: {}
//
// Besides port, path, private, data, env, health, needs and aliases (other
// names the service answers to inside the stack), a service may carry any
// compose key (command, user, volumes…), copied as written.

// blueprintKeys are the keys of a blueprint service that are not compose keys.
var blueprintKeys = map[string]bool{
	"port": true, "path": true, "private": true, "data": true, "env": true, "health": true, "needs": true,
	"aliases": true,
}

// placeholder is a reference to a component's connection detail.
var placeholder = regexp.MustCompile(`\{\{([a-z0-9][a-z0-9_-]*)\.([a-z_]+)\}\}`)

// blueprintService is one application service while it is being expanded.
type blueprintService struct {
	name       string
	definition orderedMap
	port       int
	path       string
	private    bool
	needs      []string
	healthSpec any
}

// expandBlueprint turns the blueprint of a template into its compose file and,
// when the template does not list them, its routes.
func expandBlueprint(template *Template) error {
	if template.Services.Kind == 0 {
		return nil
	}
	if strings.TrimSpace(template.Compose) != "" {
		return fmt.Errorf("a template has either services (a blueprint) or compose, not both")
	}
	if template.Services.Kind != yaml.MappingNode || len(template.Services.Content) == 0 {
		return fmt.Errorf("services must be a mapping of at least one service")
	}
	components, fields, err := expandComponents(template.With)
	if err != nil {
		return err
	}
	taken := map[string]bool{}
	for _, component := range components {
		taken[component.name] = true
	}
	services := []blueprintService{}
	for index := 0; index < len(template.Services.Content); index += 2 {
		name := template.Services.Content[index].Value
		if taken[name] {
			return fmt.Errorf("service %q is also the name of a component", name)
		}
		taken[name] = true
		service, err := readService(name, template.Services.Content[index+1])
		if err != nil {
			return err
		}
		services = append(services, service)
	}
	written := []namedService{}
	for _, service := range services {
		if err := finishService(&service, components, taken); err != nil {
			return err
		}
		written = append(written, namedService{service.name, service.definition})
	}
	written = append(written, components...)
	for _, service := range written {
		replaced, err := fillPlaceholders(service.definition, fields)
		if err != nil {
			return fmt.Errorf("service %q: %w", service.name, err)
		}
		service.definition.values = replaced.values
	}
	document := newMap()
	all := newMap()
	for _, service := range written {
		all.set(service.name, service.definition)
	}
	document.set("services", all)
	var raw strings.Builder
	encoder := yaml.NewEncoder(&raw)
	encoder.SetIndent(2)
	if err := encoder.Encode(toNode(document)); err != nil {
		return err
	}
	template.Compose = raw.String()
	if template.Routes == nil {
		template.Routes = []TemplateRoute{}
		for _, service := range services {
			if service.port > 0 && !service.private {
				template.Routes = append(template.Routes, TemplateRoute{Service: service.name, Port: service.port, Path: service.path})
			}
		}
	}
	return nil
}

func expandComponents(declared map[string]ComponentOptions) ([]namedService, map[string]map[string]string, error) {
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)
	services := []namedService{}
	fields := map[string]map[string]string{}
	for _, name := range names {
		built, values, err := buildComponent(name, declared[name])
		if err != nil {
			return nil, nil, err
		}
		services = append(services, built...)
		fields[name] = values
	}
	return services, fields, nil
}

// readService separates the blueprint keys of a service from the compose keys,
// which are kept in the order they were written.
func readService(name string, node *yaml.Node) (blueprintService, error) {
	service := blueprintService{name: name, definition: newMap()}
	if node.Kind != yaml.MappingNode {
		return service, fmt.Errorf("service %q must be a mapping", name)
	}
	var volumes []any
	var environment []any
	for index := 0; index < len(node.Content); index += 2 {
		key, value := node.Content[index].Value, node.Content[index+1]
		var decoded any
		if err := value.Decode(&decoded); err != nil {
			return service, fmt.Errorf("service %q, %s: %w", name, key, err)
		}
		switch key {
		case "port":
			port, isInt := decoded.(int)
			if !isInt || port < 1 || port > 65535 {
				return service, fmt.Errorf("service %q: port must be a number between 1 and 65535", name)
			}
			service.port = port
		case "path":
			service.path = fmt.Sprint(decoded)
		case "private":
			service.private, _ = decoded.(bool)
		case "data":
			path, isString := decoded.(string)
			if !isString || !strings.HasPrefix(path, "/") {
				return service, fmt.Errorf("service %q: data must be an absolute path in the container", name)
			}
			volumes = append(volumes, name+"-data:"+path)
		case "env":
			if value.Kind != yaml.MappingNode {
				return service, fmt.Errorf("service %q: env must be a mapping", name)
			}
			for item := 0; item < len(value.Content); item += 2 {
				environment = append(environment, value.Content[item].Value+"="+value.Content[item+1].Value)
			}
		case "health":
			service.healthSpec = decoded
		case "needs":
			list, isList := decoded.([]any)
			if !isList {
				return service, fmt.Errorf("service %q: needs must be a list of services", name)
			}
			service.needs = []string{}
			for _, need := range list {
				service.needs = append(service.needs, fmt.Sprint(need))
			}
		case "aliases":
			list, isList := decoded.([]any)
			if !isList || len(list) == 0 {
				return service, fmt.Errorf("service %q: aliases must be a list of names", name)
			}
			network := newMap()
			network.set("aliases", list)
			networks := newMap()
			networks.set("default", network)
			service.definition.set("networks", networks)
		case "networks":
			return service, fmt.Errorf("service %q: a stack has one network; use aliases for extra names", name)
		case "environment":
			return service, fmt.Errorf("service %q: write the variables under env", name)
		case "volumes":
			list, isList := decoded.([]any)
			if !isList {
				return service, fmt.Errorf("service %q: volumes must be a list", name)
			}
			volumes = append(list, volumes...)
		default:
			service.definition.set(key, decoded)
		}
	}
	if len(environment) > 0 {
		service.definition.set("environment", environment)
	}
	if len(volumes) > 0 {
		service.definition.set("volumes", volumes)
	}
	return service, nil
}

// finishService adds the health check and the dependencies of a service.
func finishService(service *blueprintService, components []namedService, known map[string]bool) error {
	if service.healthSpec != nil {
		test, err := healthTest(service.healthSpec, service.port)
		if err != nil {
			return fmt.Errorf("service %q: %w", service.name, err)
		}
		check := newMap()
		check.set("test", test)
		check.set("interval", "10s")
		check.set("timeout", "5s")
		check.set("retries", 5)
		check.set("start_period", "30s")
		service.definition.set("healthcheck", check)
	}
	if service.definition.has("depends_on") {
		return nil
	}
	conditions := map[string]string{}
	for _, component := range components {
		condition := "service_healthy"
		if !component.definition.has("healthcheck") {
			condition = "service_completed_successfully"
		}
		conditions[component.name] = condition
	}
	needs := service.needs
	if needs == nil {
		for _, component := range components {
			needs = append(needs, component.name)
		}
	}
	if len(needs) == 0 {
		return nil
	}
	dependencies := newMap()
	for _, need := range needs {
		if !known[need] {
			return fmt.Errorf("service %q needs %q, which the template does not define", service.name, need)
		}
		condition := conditions[need]
		if condition == "" {
			condition = "service_started"
		}
		entry := newMap()
		entry.set("condition", condition)
		dependencies.set(need, entry)
	}
	service.definition.set("depends_on", dependencies)
	return nil
}

// healthTest reads the health shorthand of a service:
//
//	wget:/path   HTTP GET on the service port with wget
//	curl:/path   the same with curl
//	tcp          the port accepts connections (needs bash in the image)
//	nc           the same with netcat
//	cmd:<shell>  any shell command
//	[CMD, …]     the test exactly as compose takes it
func healthTest(spec any, port int) ([]any, error) {
	if list, isList := spec.([]any); isList {
		return list, nil
	}
	text, isString := spec.(string)
	if !isString {
		return nil, fmt.Errorf("health must be text or a list")
	}
	needsPort := func() error {
		if port == 0 {
			return fmt.Errorf("health %q needs the service port", text)
		}
		return nil
	}
	address := "127.0.0.1:" + strconv.Itoa(port)
	switch {
	case strings.HasPrefix(text, "wget:"):
		if err := needsPort(); err != nil {
			return nil, err
		}
		return []any{"CMD-SHELL", "wget -q --spider http://" + address + strings.TrimPrefix(text, "wget:") + " || exit 1"}, nil
	case strings.HasPrefix(text, "curl:"):
		if err := needsPort(); err != nil {
			return nil, err
		}
		return []any{"CMD-SHELL", "curl -fsS -o /dev/null http://" + address + strings.TrimPrefix(text, "curl:") + " || exit 1"}, nil
	case text == "tcp":
		if err := needsPort(); err != nil {
			return nil, err
		}
		return []any{"CMD-SHELL", "bash -c ':> /dev/tcp/127.0.0.1/" + strconv.Itoa(port) + "' || exit 1"}, nil
	case text == "nc":
		if err := needsPort(); err != nil {
			return nil, err
		}
		return []any{"CMD-SHELL", "nc -z 127.0.0.1 " + strconv.Itoa(port) + " || exit 1"}, nil
	case strings.HasPrefix(text, "cmd:"):
		return []any{"CMD-SHELL", strings.TrimSpace(strings.TrimPrefix(text, "cmd:"))}, nil
	}
	return nil, fmt.Errorf("unknown health %q (use wget:/path, curl:/path, tcp, nc or cmd:…)", text)
}

// fillPlaceholders replaces {{component.field}} in every text of a service,
// inline file contents included.
func fillPlaceholders(service orderedMap, fields map[string]map[string]string) (orderedMap, error) {
	var failure error
	var walk func(value any) any
	walk = func(value any) any {
		switch typed := value.(type) {
		case string:
			return placeholder.ReplaceAllStringFunc(typed, func(match string) string {
				parts := placeholder.FindStringSubmatch(match)
				component, isComponent := fields[parts[1]]
				if !isComponent {
					failure = fmt.Errorf("%s refers to %q, which is not a component of the template", match, parts[1])
					return match
				}
				field, has := component[parts[2]]
				if !has {
					failure = fmt.Errorf("%s: component %q has no field %q", match, parts[1], parts[2])
				}
				return field
			})
		case []any:
			for index, item := range typed {
				typed[index] = walk(item)
			}
		case map[string]any:
			for key, item := range typed {
				typed[key] = walk(item)
			}
		case orderedMap:
			for _, key := range typed.keys {
				typed.values[key] = walk(typed.values[key])
			}
		}
		return value
	}
	walk(service)
	return service, failure
}

// toNode writes a value as YAML, keeping the order of ordered maps and
// sorting plain ones.
func toNode(value any) *yaml.Node {
	switch typed := value.(type) {
	case orderedMap:
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, key := range typed.keys {
			node.Content = append(node.Content, scalar(key), toNode(typed.values[key]))
		}
		return node
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		node := &yaml.Node{Kind: yaml.MappingNode}
		for _, key := range keys {
			node.Content = append(node.Content, scalar(key), toNode(typed[key]))
		}
		return node
	case []any:
		node := &yaml.Node{Kind: yaml.SequenceNode}
		for _, item := range typed {
			node.Content = append(node.Content, toNode(item))
		}
		return node
	}
	return scalar(value)
}

func scalar(value any) *yaml.Node {
	node := &yaml.Node{}
	if err := node.Encode(value); err != nil {
		return &yaml.Node{Kind: yaml.ScalarNode, Value: fmt.Sprint(value)}
	}
	return node
}
