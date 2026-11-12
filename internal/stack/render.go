package stack

import (
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

var serviceNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// document is a compose file parsed loosely, so keys Pier does not know
// about are preserved untouched.
type document map[string]any

func parseCompose(raw string) (document, error) {
	// Decoding into the plain map type keeps nested mappings as map[string]any
	// (yaml.v3 would otherwise reuse the named type for them).
	var plain map[string]any
	if err := yaml.Unmarshal([]byte(raw), &plain); err != nil {
		return nil, apperr.Invalid("O compose não é um YAML válido: %s", err)
	}
	doc := document(plain)
	services, ok := doc["services"].(map[string]any)
	if !ok || len(services) == 0 {
		return nil, apperr.Invalid("O compose precisa declarar ao menos um serviço em \"services\".")
	}
	for name, definition := range services {
		if !serviceNamePattern.MatchString(name) {
			return nil, apperr.Invalid("Nome de serviço inválido: %q.", name)
		}
		service, ok := definition.(map[string]any)
		if !ok {
			return nil, apperr.Invalid("O serviço %q não tem uma definição válida.", name)
		}
		normalizeEnvironment(service)
	}
	return doc, nil
}

// normalizeEnvironment repairs an "environment" that is a list written as a
// mapping with the positions as keys (0: A=1, 1: B=2), possibly mixed with
// ordinary entries (C: 3). Some converted templates come like that, an
// artifact of how their source was generated from YAML anchors; docker
// compose refuses it. The result is a plain list, positions first.
func normalizeEnvironment(service map[string]any) {
	// With keys that are not strings, YAML decodes the mapping as map[any]any.
	environment, isMixed := service["environment"].(map[any]any)
	if !isMixed {
		return
	}
	positions, names := []int{}, []string{}
	for key := range environment {
		switch typed := key.(type) {
		case int:
			positions = append(positions, typed)
		case string:
			names = append(names, typed)
		default:
			return
		}
	}
	sort.Ints(positions)
	sort.Strings(names)
	entries := make([]any, 0, len(environment))
	for _, position := range positions {
		entries = append(entries, fmt.Sprint(environment[position]))
	}
	for _, name := range names {
		if value := environment[name]; value != nil {
			entries = append(entries, name+"="+fmt.Sprint(value))
		} else {
			entries = append(entries, name)
		}
	}
	service["environment"] = entries
}

func (d document) serviceNames() []string {
	services, _ := d["services"].(map[string]any)
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// ingressKey is the compose key of the network shared with the proxy.
const ingressKey = "pier_ingress"

// IngressNetwork is the docker network through which the proxy reaches the
// published services of a stack. Only routed services join it, so service
// names of different stacks never collide in DNS.
func IngressNetwork(stackID string) string { return stackID + "_ingress" }

// hostFile is a file or directory the compose file expects to find next to
// it on the server ("content:" and "is_directory:" on a bind mount).
type hostFile struct {
	// Path is relative to the directory of the stack.
	Path    string
	Content []byte
	Mode    fs.FileMode
}

// render turns the user's compose into the file Pier actually runs: every
// service gets identification labels and, when it has routes, joins the
// ingress network and receives the proxy labels that publish it. Extensions
// docker compose does not understand are resolved or removed.
func render(stack Stack) ([]byte, []hostFile, error) {
	doc, err := parseCompose(stack.Compose)
	if err != nil {
		return nil, nil, err
	}
	files := []hostFile{}
	services := doc["services"].(map[string]any)
	for name, definition := range services {
		service := definition.(map[string]any)
		publication, err := publish(stack, name, service["networks"], hasKey(service, "network_mode"))
		if err != nil {
			return nil, nil, err
		}
		service["labels"] = mergeLabels(service["labels"], publication.labels)
		if publication.networks != nil {
			service["networks"] = publication.networks
		}
		if _, hasRestart := service["restart"]; !hasRestart {
			service["restart"] = "unless-stopped"
		}
		if !hasKey(service, "logging") {
			service["logging"] = compose.DefaultLogging()
		}
		qualifyRelativeBinds(service)
		serviceFiles, err := resolveHostFiles(name, service)
		if err != nil {
			return nil, nil, err
		}
		files = append(files, serviceFiles...)
	}
	doc["networks"] = withIngress(doc["networks"], stack.ID)
	declareVolumes(doc)
	escapeForeignDollars(map[string]any(doc))
	rendered, err := marshalCompose(doc)
	return rendered, files, err
}

func marshalCompose(doc map[string]any) ([]byte, error) { return yaml.Marshal(doc) }

func hasKey(mapping map[string]any, key string) bool {
	_, found := mapping[key]
	return found
}

// publication is what Pier adds to one service of a stack.
type publication struct {
	labels map[string]string
	// networks is nil when the service keeps the networks it declared.
	networks map[string]any
}

// publish computes the labels of a service and, when it has routes, the
// networks that make it reachable by the proxy.
func publish(stack Stack, name string, declaredNetworks any, usesNetworkMode bool) (publication, error) {
	result := publication{labels: docker.Labels(docker.TypeStack, stack.ID)}
	routes, port, err := routesFor(stack, name)
	if err != nil || len(routes) == 0 {
		return result, err
	}
	if usesNetworkMode {
		return result, apperr.Invalid("O serviço %q usa network_mode e não pode receber domínio.", name)
	}
	routerName := stack.ID + "-" + strings.ToLower(variableSuffix(name))
	publication := compose.Publication{
		Name: routerName, Network: IngressNetwork(stack.ID), Routes: routes, Port: port, Entry: stack.proxyEntry,
	}
	for key, value := range publication.Labels() {
		result.labels[key] = compose.EscapeInterpolation(value)
	}
	for key, value := range stack.proxyLabels {
		result.labels[key] = value
	}
	result.networks = joinIngress(declaredNetworks)
	return result, nil
}

func withIngress(existing any, stackID string) map[string]any {
	networks, _ := existing.(map[string]any)
	if networks == nil {
		networks = map[string]any{}
	}
	networks[ingressKey] = map[string]any{
		"name":   IngressNetwork(stackID),
		"labels": map[string]any{docker.LabelIngress: "true"},
	}
	return networks
}

func routesFor(stack Stack, service string) ([]compose.Route, int, error) {
	parsed := []compose.Route{}
	port := 0
	for _, route := range stack.Routes {
		if route.Service != service {
			continue
		}
		proxyRoute, err := compose.ParseRoute(route.Domain)
		if err != nil {
			return nil, 0, i18n.Errorf("domínio %q: %w", route.Domain, err)
		}
		if stack.pendingHosts[proxyRoute.Host] && proxyRoute.Scheme == "https" {
			proxyRoute.Scheme = "http"
		}
		parsed = append(parsed, proxyRoute)
		port = route.Port
	}
	return parsed, port, nil
}

// mergeLabels accepts both compose label syntaxes (list and map).
func mergeLabels(existing any, extra map[string]string) map[string]any {
	merged := map[string]any{}
	switch labels := existing.(type) {
	case map[string]any:
		for key, value := range labels {
			merged[key] = value
		}
	case []any:
		for _, item := range labels {
			key, value, _ := strings.Cut(fmt.Sprint(item), "=")
			merged[key] = value
		}
	}
	for key, value := range extra {
		merged[key] = value
	}
	return merged
}

// joinIngress adds the ingress network while keeping the networks the service
// already had (the implicit "default" one included), so services of the stack
// still resolve each other by name.
func joinIngress(existing any) map[string]any {
	networks := map[string]any{}
	switch declared := existing.(type) {
	case map[string]any:
		for name, options := range declared {
			networks[name] = options
		}
	case []any:
		for _, name := range declared {
			networks[fmt.Sprint(name)] = nil
		}
	default:
		networks["default"] = nil
	}
	networks[ingressKey] = nil
	return networks
}

// qualifyRelativeBinds turns a short mount such as "data/:/var/lib/app" into
// "./data/:/var/lib/app". Without the "./", docker compose takes the source
// for a volume name, and "data/" is not a valid one.
func qualifyRelativeBinds(service map[string]any) {
	mounts, _ := service["volumes"].([]any)
	for index, mount := range mounts {
		entry, isShort := mount.(string)
		if !isShort {
			continue
		}
		source, rest, hasTarget := strings.Cut(entry, ":")
		if !hasTarget || source == "" || strings.ContainsAny(source[:1], "./~$") || volumeName.MatchString(source) {
			continue
		}
		mounts[index] = "./" + source + ":" + rest
	}
}

// escapeForeignDollars doubles the "$" of every "${…}" that is not a compose
// interpolation, such as the JavaScript template literal in a health check
// (${process.env.PORT||3000}). docker compose would refuse the file; with
// "$$" the container receives the text as it was written.
func escapeForeignDollars(node any) any {
	switch value := node.(type) {
	case string:
		return foreignExpression.ReplaceAllStringFunc(value, func(match string) string {
			if strings.HasPrefix(match, "$$") || composeExpression.MatchString(match) {
				return match
			}
			return "$" + match
		})
	case []any:
		for index, item := range value {
			value[index] = escapeForeignDollars(item)
		}
	case map[string]any:
		for key, item := range value {
			value[key] = escapeForeignDollars(item)
		}
	}
	return node
}

var (
	// foreignExpression finds "${…" up to the first closing brace, and the
	// escaped form "$${", which is left alone.
	foreignExpression = regexp.MustCompile(`\$?\$\{[^}]*\}?`)
	// composeExpression is what compose interpolates: a name, optionally
	// followed by a default, error or alternative operator.
	composeExpression = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*(?:(?::?[-?+]).*)?\}?$`)
)

// declareVolumes adds to the top-level "volumes" every named volume a
// service uses without declaring; docker compose refuses the file without
// them, and hand-written files often leave the section out.
func declareVolumes(doc document) {
	declared, _ := doc["volumes"].(map[string]any)
	if declared == nil {
		declared = map[string]any{}
	}
	services, _ := doc["services"].(map[string]any)
	for _, definition := range services {
		service, _ := definition.(map[string]any)
		mounts, _ := service["volumes"].([]any)
		for _, mount := range mounts {
			if name := namedVolume(mount); name != "" && !hasKey(declared, name) {
				declared[name] = nil
			}
		}
	}
	if len(declared) > 0 {
		doc["volumes"] = declared
	}
}

var volumeName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)

// namedVolume returns the volume a mount refers to, or "" when the mount is
// a bind (a path), an anonymous volume or anything else.
func namedVolume(mount any) string {
	source := ""
	switch entry := mount.(type) {
	case string:
		// "name:/target[:mode]"; a single segment is an anonymous volume.
		first, _, hasTarget := strings.Cut(entry, ":")
		if !hasTarget {
			return ""
		}
		source = first
	case map[string]any:
		if kind, _ := entry["type"].(string); kind != "volume" {
			return ""
		}
		source, _ = entry["source"].(string)
	}
	if !volumeName.MatchString(source) {
		return ""
	}
	return source
}

// resolveHostFiles handles Pier's mount extensions. A bind mount of a
// template may carry the content of the file it mounts ("content:") or say
// that its source is a directory to be created ("is_directory:"); docker
// compose knows neither. The content becomes a file Pier writes next to the
// compose file.
func resolveHostFiles(serviceName string, service map[string]any) ([]hostFile, error) {
	files := []hostFile{}
	volumes, _ := service["volumes"].([]any)
	for _, entry := range volumes {
		mount, isLongSyntax := entry.(map[string]any)
		if !isLongSyntax {
			continue
		}
		content, hasContent := mount["content"]
		extended := hasContent
		mode := fileMode(mount["mode"])
		for _, extension := range []string{"content", "is_directory", "isDirectory", "mode"} {
			if hasKey(mount, extension) {
				extended = true
			}
			delete(mount, extension)
		}
		if mount["type"] != "bind" {
			continue
		}
		rawSource := fmt.Sprint(mount["source"])
		if !hasContent {
			// A bind mount in the long syntax fails when its source is
			// missing, while the short syntax creates the directory; ask
			// docker for the same.
			if extended || strings.HasPrefix(rawSource, "./") {
				createHostPath(mount)
			}
			continue
		}
		source := path.Clean(rawSource)
		if path.IsAbs(source) || source == ".." || strings.HasPrefix(source, "../") || strings.Contains(source, "$") {
			return nil, apperr.Invalid("O serviço %q monta um arquivo com conteúdo em %q; use um caminho relativo "+
				"(./arquivo), que fica na pasta da stack.", serviceName, rawSource)
		}
		text := fmt.Sprint(content)
		if mode == 0 {
			mode = 0o644
			if strings.HasPrefix(text, "#!") {
				mode = 0o755
			}
		}
		files = append(files, hostFile{Path: source, Content: []byte(text), Mode: mode})
	}
	return files, nil
}

func createHostPath(mount map[string]any) {
	options, _ := mount["bind"].(map[string]any)
	if options == nil {
		options = map[string]any{}
	}
	if !hasKey(options, "create_host_path") {
		options["create_host_path"] = true
	}
	mount["bind"] = options
}

// fileMode reads a mode written as "0755" (or as the number YAML made of it).
func fileMode(value any) fs.FileMode {
	if value == nil {
		return 0
	}
	parsed, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprint(value)), 8, 32)
	if err != nil {
		return 0
	}
	return fs.FileMode(parsed) & fs.ModePerm
}
