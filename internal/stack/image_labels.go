package stack

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"pier/internal/platform/apperr"
	"pier/internal/platform/shell"
)

// The proxy reads its routes from container labels, and Pier tracks resources
// by its own labels. An image (or a compose file) that carries traefik.* or
// pier.* labels could therefore publish a service on someone else's domain or
// impersonate a managed resource. Routed services are checked before "up":
// the labels written in the compose file, and the ones the image inherits.
// Only the labels Pier generates itself are accepted.

// resolvedService is what `docker compose config` reports for a service.
type resolvedService struct {
	Image  string         `json:"image"`
	Labels map[string]any `json:"labels"`
}

// verifyRoutedServices refuses the deployment when a routed service carries
// reserved labels, either declared in the compose file or inherited from its
// image. It runs after pull and build, so the images exist locally.
func verifyRoutedServices(ctx context.Context, run *stackRun) error {
	routed := routedServices(run.stack)
	if len(routed) == 0 {
		return nil
	}
	resolved, err := resolveServices(ctx, run)
	if err != nil {
		return err
	}
	for _, name := range routed {
		service, found := resolved[name]
		if !found {
			continue
		}
		if err := checkDeclaredLabels(run.stack, name, service.Labels); err != nil {
			return err
		}
		if err := checkImageLabels(ctx, run, name, imageOf(run.stack, name, service)); err != nil {
			return err
		}
	}
	return nil
}

func routedServices(stack Stack) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, route := range stack.Routes {
		if !seen[route.Service] {
			seen[route.Service] = true
			names = append(names, route.Service)
		}
	}
	sort.Strings(names)
	return names
}

func resolveServices(ctx context.Context, run *stackRun) (map[string]resolvedService, error) {
	output, err := run.ex.Run(ctx, run.project.Command("config", "--format", "json"))
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível ler a configuração resolvida da stack para verificar as etiquetas: %s", lastLine(err))
	}
	var config struct {
		Services map[string]resolvedService `json:"services"`
	}
	if err := json.Unmarshal([]byte(output), &config); err != nil {
		return nil, apperr.Unavailable("A configuração resolvida da stack é inválida; a operação foi interrompida.")
	}
	return config.Services, nil
}

// imageOf is the image a service runs; a service built from source gets the
// name docker compose derives from the project.
func imageOf(stack Stack, name string, service resolvedService) string {
	if service.Image != "" {
		return service.Image
	}
	return stack.composeProject() + "-" + name
}

func isReservedLabel(key string) bool {
	lower := strings.ToLower(key)
	return strings.HasPrefix(lower, "traefik.") || strings.HasPrefix(lower, "pier.")
}

// checkDeclaredLabels rejects reserved labels of the service that Pier did
// not generate itself.
func checkDeclaredLabels(stack Stack, name string, labels map[string]any) error {
	generated, err := publish(stack, name, nil, false)
	if err != nil {
		return err
	}
	forbidden := []string{}
	for key := range labels {
		if _, own := generated.labels[key]; !own && isReservedLabel(key) {
			forbidden = append(forbidden, key)
		}
	}
	return reservedLabelsError(name, forbidden, false, "")
}

func checkImageLabels(ctx context.Context, run *stackRun, name, image string) error {
	output, err := run.ex.Run(ctx, shell.Join("docker", "image", "inspect", "--format", "{{json .Config.Labels}}", image))
	if err != nil {
		return apperr.Unavailable("Não foi possível verificar as etiquetas da imagem %s do serviço %q: %s", image, name, lastLine(err))
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &labels); err != nil {
		return apperr.Unavailable("A imagem %s do serviço %q retornou etiquetas inválidas; a operação foi interrompida.", image, name)
	}
	forbidden := []string{}
	for key := range labels {
		if isReservedLabel(key) {
			forbidden = append(forbidden, key)
		}
	}
	return reservedLabelsError(name, forbidden, true, image)
}

func reservedLabelsError(service string, forbidden []string, inImage bool, image string) error {
	if len(forbidden) == 0 {
		return nil
	}
	sort.Strings(forbidden)
	labels := strings.Join(forbidden, ", ")
	if inImage {
		return apperr.Invalid("O serviço %q usa a imagem %s, que traz etiquetas reservadas (%s). Use uma imagem sem etiquetas "+
			"traefik.* e pier.*: elas podem alterar rotas e recursos gerenciados pelo Pier.", service, image, labels)
	}
	return apperr.Invalid("O serviço %q declara etiquetas reservadas no compose (%s). Remova as etiquetas traefik.* e pier.*: "+
		"elas podem alterar rotas e recursos gerenciados pelo Pier.", service, labels)
}
