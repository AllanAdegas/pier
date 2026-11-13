package terminal

import (
	"context"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

const (
	composeServiceLabel = "com.docker.compose.service"
	processLabel        = "pier.process"
	webProcess          = "web"
	minimumIDPrefix     = 12
)

// candidate is one container as `docker ps` reports it, with the labels
// that tell which resource owns it.
type candidate struct {
	id, name, state, managed, kind, resourceID, service, process string
}

var candidateFormat = docker.FormatColumns("{{.ID}}", "{{.Names}}", "{{.State}}",
	`{{.Label "pier.managed"}}`, `{{.Label "pier.type"}}`, `{{.Label "pier.id"}}`,
	`{{.Label "`+composeServiceLabel+`"}}`, `{{.Label "`+processLabel+`"}}`)

// resolve finds the container the request targets and proves it belongs to
// the resource of the route. The container id it returns comes from docker,
// never from the request, and is the only one that reaches `docker exec`.
func resolve(ctx context.Context, ex executor.Executor, request Request) (Container, error) {
	filter := "label=" + docker.LabelID + "=" + request.ResourceID
	if request.Kind == KindContainer {
		filter = ""
	}
	candidates, err := listCandidates(ctx, ex, filter)
	if err != nil {
		return Container{}, err
	}
	if request.Kind == KindContainer {
		return pickByName(candidates, request.ResourceID)
	}
	owned := ownedBy(candidates, request)
	if request.Container != "" {
		return pickByName(owned, request.Container)
	}
	if request.Kind == KindStack {
		return pickService(owned, request.Service)
	}
	return pickDefault(owned)
}

func listCandidates(ctx context.Context, ex executor.Executor, filter string) ([]candidate, error) {
	args := []string{"docker", "ps", "--all", "--no-trunc", "--format", candidateFormat}
	if filter != "" {
		args = append(args, "--filter", filter)
	}
	output, err := ex.Run(ctx, shell.Join(args...))
	if err != nil {
		return nil, apperr.Unavailable("Não foi possível consultar o Docker: %s", lastLine(err.Error()))
	}
	var candidates []candidate
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) < 3 || !docker.ValidObjectName(fields[0]) {
			continue
		}
		fields = append(fields, make([]string, 8)...)
		candidates = append(candidates, candidate{id: fields[0], name: fields[1], state: fields[2], managed: fields[3],
			kind: fields[4], resourceID: fields[5], service: fields[6], process: fields[7]})
	}
	return candidates, nil
}

// ownedBy keeps the containers Pier manages for exactly this resource. The
// docker filter already narrowed by id; the labels are checked again here.
func ownedBy(candidates []candidate, request Request) []candidate {
	var owned []candidate
	for _, c := range candidates {
		if c.managed == "true" && c.kind == string(request.Kind) && c.resourceID == request.ResourceID {
			owned = append(owned, c)
		}
	}
	return owned
}

func pickByName(candidates []candidate, idOrName string) (Container, error) {
	for _, c := range candidates {
		if c.id == idOrName || c.name == idOrName || len(idOrName) >= minimumIDPrefix && strings.HasPrefix(c.id, idOrName) {
			return running(c)
		}
	}
	return Container{}, apperr.NotFound("Container")
}

func pickService(candidates []candidate, service string) (Container, error) {
	if service == "" {
		services := map[string]bool{}
		for _, c := range candidates {
			services[c.service] = true
		}
		if len(services) > 1 {
			return Container{}, apperr.Invalid("Escolha o serviço da stack.")
		}
		return pickDefault(candidates)
	}
	var matching []candidate
	for _, c := range candidates {
		if c.service == service {
			matching = append(matching, c)
		}
	}
	if len(matching) == 0 {
		return Container{}, apperr.NotFound("Serviço")
	}
	return pickDefault(matching)
}

// pickDefault prefers a running container of the web process.
func pickDefault(candidates []candidate) (Container, error) {
	var fallback *candidate
	for i, c := range candidates {
		if c.state != "running" {
			continue
		}
		if c.process == "" || c.process == webProcess {
			return running(c)
		}
		if fallback == nil {
			fallback = &candidates[i]
		}
	}
	if fallback != nil {
		return running(*fallback)
	}
	if len(candidates) == 0 {
		return Container{}, apperr.Conflict("Nenhum container deste recurso foi encontrado no servidor. Faça um deploy primeiro.")
	}
	return Container{}, apperr.Conflict("O container não está em execução. Inicie o recurso e tente de novo.")
}

func running(c candidate) (Container, error) {
	if c.state != "running" {
		return Container{}, apperr.Conflict("O container não está em execução. Inicie o recurso e tente de novo.")
	}
	return Container{ID: c.id, Name: c.name, Service: c.service}, nil
}

func lastLine(text string) string {
	text = strings.TrimSpace(text)
	return text[strings.LastIndex(text, "\n")+1:]
}
