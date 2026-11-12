package application

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// repoConfigNames are looked up, in order, at the base directory.
var repoConfigNames = []string{"pier.yaml", ".pier.yaml"}

const maxRepoConfigBytes = 64 << 10

// repoConfig is the strict schema of pier.yaml. Pointers tell a field that
// is absent from one set to its zero value.
type repoConfig struct {
	Build struct {
		Pack             *BuildPack `yaml:"pack"`
		Dockerfile       *string    `yaml:"dockerfile"`
		PublishDirectory *string    `yaml:"publish_directory"`
		SPA              *bool      `yaml:"spa"`
		InstallCommand   *string    `yaml:"install_command"`
		BuildCommand     *string    `yaml:"build_command"`
		StartCommand     *string    `yaml:"start_command"`
	} `yaml:"build"`
	Port   *int `yaml:"port"`
	Health struct {
		Enabled            *bool   `yaml:"enabled"`
		Path               *string `yaml:"path"`
		Command            *string `yaml:"command"`
		IntervalSeconds    *int    `yaml:"interval_seconds"`
		TimeoutSeconds     *int    `yaml:"timeout_seconds"`
		Retries            *int    `yaml:"retries"`
		StartPeriodSeconds *int    `yaml:"start_period_seconds"`
	} `yaml:"health"`
	Release struct {
		Command *string `yaml:"command"`
	} `yaml:"release"`
	Limits     *repoLimits    `yaml:"limits"`
	Processes  *[]repoProcess `yaml:"processes"`
	WatchPaths *[]string      `yaml:"watch_paths"`
}

type repoLimits struct {
	CPUs   string `yaml:"cpus"`
	Memory string `yaml:"memory"`
}

type repoProcess struct {
	Name     string     `yaml:"name"`
	Command  string     `yaml:"command"`
	Replicas int        `yaml:"replicas"`
	Limits   repoLimits `yaml:"limits"`
}

// repoConfigSchema lists the accepted keys per mapping path ("" is the root,
// "processes[]" each process).
var repoConfigSchema = map[string][]string{
	"":                   {"build", "port", "health", "release", "limits", "processes", "watch_paths"},
	"build":              {"pack", "dockerfile", "publish_directory", "spa", "install_command", "build_command", "start_command"},
	"health":             {"enabled", "path", "command", "interval_seconds", "timeout_seconds", "retries", "start_period_seconds"},
	"release":            {"command"},
	"limits":             {"cpus", "memory"},
	"processes[]":        {"name", "command", "replicas", "limits"},
	"processes[].limits": {"cpus", "memory"},
}

// panelOnlyKeys never come from the repository: who can push code must not
// be able to move the application, publish domains or read secrets.
var panelOnlyKeys = map[string]bool{
	"source": true, "repository": true, "branch": true, "tag": true, "server": true, "environment": true,
	"network": true, "domains": true, "port_mappings": true, "env": true, "environment_variables": true,
	"variables": true, "secrets": true, "storages": true, "volumes": true, "hooks": true,
}

// overlayRepoConfig parses pier.yaml and applies it over a copy of config.
// It returns the merged config and the fields the file set, in order.
func overlayRepoConfig(content []byte, config Config) (Config, []string, error) {
	if len(content) > maxRepoConfigBytes {
		return Config{}, nil, i18n.Errorf("pier.yaml passa do limite de 64 KiB")
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return Config{}, nil, repoSyntaxError(err)
	}
	if len(document.Content) == 0 {
		return config, nil, nil
	}
	root := document.Content[0]
	lines := map[string]int{}
	if err := checkRepoKeys(root, "", lines); err != nil {
		return Config{}, nil, err
	}
	var file repoConfig
	if err := root.Decode(&file); err != nil {
		return Config{}, nil, repoSyntaxError(err)
	}
	if pack := file.Build.Pack; pack != nil && !slices.Contains(GitPacks, *pack) {
		return Config{}, nil, i18n.Errorf("pier.yaml, linha %d: build.pack aceita dockerfile, nixpacks ou static", lines["build.pack"])
	}
	merged, fields := file.apply(config)
	if err := merged.Validate(); err != nil {
		return Config{}, nil, repoValidationError(err, lines)
	}
	return merged, fields, nil
}

func checkRepoKeys(node *yaml.Node, schemaPath string, lines map[string]int) error {
	if schemaPath == "" && node.Kind != yaml.MappingNode {
		return i18n.Errorf("pier.yaml, linha %d: o arquivo deve ser um mapa de campos, ex.: port: 3000", node.Line)
	}
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			if err := checkRepoKeys(item, schemaPath+"[]", lines); err != nil {
				return err
			}
		}
		return nil
	case yaml.MappingNode:
	default:
		return nil
	}
	allowed, known := repoConfigSchema[schemaPath]
	if !known {
		return i18n.Errorf("pier.yaml, linha %d: %s não aceita campos aninhados", node.Line, schemaPath)
	}
	for index := 0; index+1 < len(node.Content); index += 2 {
		key, value := node.Content[index], node.Content[index+1]
		switch {
		case schemaPath == "" && panelOnlyKeys[key.Value]:
			return i18n.Errorf("pier.yaml, linha %d: %q só pode ser configurado no painel (origem, servidor, ambiente, domínios e segredos nunca vêm do repositório)", key.Line, key.Value)
		case !slices.Contains(allowed, key.Value):
			return i18n.Errorf("pier.yaml, linha %d: campo desconhecido %q; aceitos: %s", key.Line, key.Value, strings.Join(allowed, ", "))
		}
		child := strings.TrimPrefix(schemaPath+"."+key.Value, ".")
		lines[configFieldOf(child)] = key.Line
		if err := checkRepoKeys(value, child, lines); err != nil {
			return err
		}
	}
	return nil
}

// configFieldOf maps a pier.yaml key path to the Config field reported by validation.
func configFieldOf(key string) string {
	switch {
	case key == "port":
		return "network.port"
	case key == "build.dockerfile":
		return "build.dockerfile_path"
	case strings.HasPrefix(key, "processes"):
		return "processes"
	}
	return key
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

// repoSyntaxError reports the line of a YAML syntax or type error in pt-BR.
func repoSyntaxError(err error) error {
	var typeErr *yaml.TypeError
	message := err.Error()
	if errors.As(err, &typeErr) && len(typeErr.Errors) > 0 {
		message = typeErr.Errors[0]
	}
	if match := yamlLine.FindStringSubmatch(message); match != nil {
		line, _ := strconv.Atoi(match[1])
		if typeErr != nil {
			return i18n.Errorf("pier.yaml, linha %d: tipo de valor inválido para este campo", line)
		}
		return i18n.Errorf("pier.yaml, linha %d: YAML inválido", line)
	}
	return i18n.Errorf("pier.yaml: YAML inválido")
}

// repoValidationError lists the validation messages with the line of each field.
func repoValidationError(err error, lines map[string]int) error {
	appErr, ok := apperr.As(err)
	if !ok || len(appErr.Fields) == 0 {
		return err
	}
	fields := make([]string, 0, len(appErr.Fields))
	for field := range appErr.Fields {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	problems := make([]string, 0, len(fields))
	for _, field := range fields {
		line := lines[field]
		if line == 0 {
			line = lines[strings.SplitN(field, ".", 2)[0]]
		}
		problems = append(problems, fmt.Sprintf("%s %d (%s): %s", i18n.Instance().Text("linha"), line, field, appErr.Fields[field]))
	}
	return i18n.Errorf("pier.yaml inválido: %s", strings.Join(problems, "; "))
}

// GitPacks are the build packs that build from source code.
var GitPacks = []BuildPack{PackDockerfile, PackNixpacks, PackStatic}
