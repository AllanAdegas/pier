package application

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/shell"
)

const (
	// LabelProcess names the process a container runs.
	LabelProcess   = "pier.process"
	maxProcesses   = 10
	maxReplicas    = 10
	processesField = "processes"
)

var processNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,29}$`)

// processList returns the processes to run: an empty list is the single web
// process every application had before processes existed.
func (c Config) processList() []Process {
	if len(c.Processes) == 0 {
		return []Process{{Name: WebProcess, Replicas: 1}}
	}
	return c.Processes
}

func (c Config) hasWebProcess() bool {
	for _, process := range c.processList() {
		if process.Name == WebProcess {
			return true
		}
	}
	return false
}

func (c *Config) normalizeProcesses() {
	if c.Processes == nil {
		c.Processes = []Process{}
	}
	for index := range c.Processes {
		process := &c.Processes[index]
		process.Name = strings.TrimSpace(process.Name)
		process.Command = strings.TrimSpace(process.Command)
		process.Limits.CPUs = strings.TrimSpace(process.Limits.CPUs)
		process.Limits.Memory = strings.TrimSpace(process.Limits.Memory)
		if process.Replicas == 0 {
			process.Replicas = 1
		}
	}
}

func (c *Config) validateProcesses(fields apperr.Fields) {
	if len(c.Processes) > maxProcesses {
		fields.Add(processesField, "Use no máximo %d processos.", maxProcesses)
	}
	seen := map[string]bool{}
	for _, process := range c.Processes {
		switch {
		case !processNamePattern.MatchString(process.Name):
			fields.Add(processesField, "Nome de processo inválido: %q. Use letras minúsculas, números e '-', ex.: worker.", process.Name)
		case seen[process.Name]:
			fields.Add(processesField, "Processo duplicado: %s.", process.Name)
		case process.Name != WebProcess && process.Command == "":
			fields.Add(processesField, "Informe o comando do processo %s.", process.Name)
		case process.Replicas < 1 || process.Replicas > maxReplicas:
			fields.Add(processesField, "O processo %s deve ter de 1 a %d réplicas.", process.Name, maxReplicas)
		case !validLimits(process.Limits):
			fields.Add(processesField, "Limites inválidos no processo %s: use CPUs como 0.5 e memória como 512m.", process.Name)
		case process.Name == WebProcess && process.Replicas > 1 && len(c.Network.PortMappings) > 0:
			fields.Add(processesField, "Portas publicadas no host exigem uma única réplica do web.")
		}
		seen[process.Name] = true
	}
	if !c.hasWebProcess() && (len(c.Network.Domains) > 0 || len(c.Network.PortMappings) > 0) {
		fields.Add(processesField, "Domínios e portas publicadas exigem um processo web.")
	}
}

func validLimits(limits Limits) bool {
	return (limits.CPUs == "" || cpusPattern.MatchString(limits.CPUs)) &&
		(limits.Memory == "" || memoryPattern.MatchString(limits.Memory))
}

// replica is one container of a release.
type replica struct {
	Name    string
	Process Process
}

// replicasOf lists the containers of a release. The first web replica keeps
// the historical container name, so single-process applications look the
// same as before.
func replicasOf(config Config, containerName string) []replica {
	replicas := []replica{}
	for _, process := range config.processList() {
		for number := 1; number <= process.Replicas; number++ {
			name := containerName
			if process.Name != WebProcess || number > 1 {
				name += "-" + process.Name
			}
			if number > 1 {
				name += "-" + strconv.Itoa(number)
			}
			replicas = append(replicas, replica{Name: name, Process: process})
		}
	}
	return replicas
}

// primaryReplica is where hooks, the release command and listener
// diagnostics run: the first web replica, or the first process without web.
func primaryReplica(replicas []replica) replica {
	for _, candidate := range replicas {
		if candidate.Process.Name == WebProcess {
			return candidate
		}
	}
	return replicas[0]
}

// ApplicationContainer is a container with the process it runs.
type ApplicationContainer struct {
	docker.Container
	Process string `json:"process"`
}

// containerProcesses maps container IDs to their process; containers created
// before processes existed have none and run the web process.
func containerProcesses(ctx context.Context, ex executor.Executor, applicationID string) map[string]string {
	output, err := ex.Run(ctx, shell.Join("docker", "ps", "-a", "--no-trunc", "--filter", "label="+docker.LabelID+"="+applicationID,
		"--format", `{{.ID}}\t{{.Label "`+LabelProcess+`"}}`))
	processes := map[string]string{}
	if err != nil {
		return processes
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Split(strings.TrimSpace(line), "\t")
		if len(fields) > 2 || fields[0] == "" {
			continue
		}
		process := WebProcess
		if len(fields) == 2 && fields[1] != "" {
			process = fields[1]
		}
		processes[fields[0]] = process
	}
	return processes
}

// withProcesses names the process of each container.
func withProcesses(containers []docker.Container, processes map[string]string) []ApplicationContainer {
	result := make([]ApplicationContainer, 0, len(containers))
	for _, container := range containers {
		process := processes[container.ID]
		if process == "" {
			process = WebProcess
		}
		result = append(result, ApplicationContainer{Container: container, Process: process})
	}
	return result
}

// runningWebContainer is the previous container hooks run in.
func runningWebContainer(containers []docker.Container, processes map[string]string) string {
	for _, container := range containers {
		process := processes[container.ID]
		if container.State == "running" && (process == "" || process == WebProcess) {
			return container.ID
		}
	}
	return ""
}
