package application

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
)

const staticPort = 80

var (
	// Only transports that cannot execute local commands (no ext::, file://).
	repositoryPattern = regexp.MustCompile(`^(https?://[^\s'"\x60]+|ssh://[^\s'"\x60]+|[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[A-Za-z0-9._~/-]+)$`)
	branchPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,199}$`)
	tagPatternSyntax  = regexp.MustCompile(`^[A-Za-z0-9*?._/\[\]-]{1,100}$`)
	commitPattern     = regexp.MustCompile(`^([0-9a-fA-F]{40}|[0-9a-fA-F]{64})$`)
	portMapPattern    = regexp.MustCompile(`^((\d{1,3}\.){3}\d{1,3}:)?\d{1,5}:\d{1,5}(/(tcp|udp))?$`)
	cpusPattern       = regexp.MustCompile(`^\d+(\.\d{1,3})?$`)
	memoryPattern     = regexp.MustCompile(`^\d+[bkmgBKMG]$`)
	volumeNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
)

// normalize trims and fills defaults so stored configs are canonical.
func (c *Config) normalize() {
	defaults := DefaultConfig(c.Build.Pack)
	c.Source.Repository = strings.TrimSpace(c.Source.Repository)
	c.Source.Branch = orDefault(strings.TrimSpace(c.Source.Branch), defaults.Source.Branch)
	c.Source.Commit = strings.TrimSpace(c.Source.Commit)
	c.Source.Image = strings.TrimSpace(c.Source.Image)
	c.Source.WatchPaths = compact(c.Source.WatchPaths)
	c.Source.Tag = strings.TrimSpace(c.Source.Tag)
	c.Source.TagPattern = strings.TrimSpace(c.Source.TagPattern)
	c.Release.Command = strings.TrimSpace(c.Release.Command)
	c.Build.BaseDirectory = cleanRelative(c.Build.BaseDirectory)
	c.Build.DockerfilePath = strings.TrimPrefix(cleanRelative(orDefault(c.Build.DockerfilePath, "Dockerfile")), "/")
	c.Build.PublishDirectory = cleanRelative(c.Build.PublishDirectory)
	c.Build.InstallCommand = strings.TrimSpace(c.Build.InstallCommand)
	c.Build.BuildCommand = strings.TrimSpace(c.Build.BuildCommand)
	c.Build.StartCommand = strings.TrimSpace(c.Build.StartCommand)
	c.Health.Path = orDefault(strings.TrimSpace(c.Health.Path), "/")
	c.Health.Command = strings.TrimSpace(c.Health.Command)
	c.Health.IntervalSeconds = positiveOr(c.Health.IntervalSeconds, defaults.Health.IntervalSeconds)
	c.Health.TimeoutSeconds = positiveOr(c.Health.TimeoutSeconds, defaults.Health.TimeoutSeconds)
	c.Health.Retries = positiveOr(c.Health.Retries, defaults.Health.Retries)
	c.Limits.CPUs = strings.TrimSpace(c.Limits.CPUs)
	c.Limits.Memory = strings.TrimSpace(c.Limits.Memory)
	c.Hooks.PreDeploy = strings.TrimSpace(c.Hooks.PreDeploy)
	c.Hooks.PostDeploy = strings.TrimSpace(c.Hooks.PostDeploy)
	c.Network.Domains = compact(c.Network.Domains)
	c.Network.PortMappings = compact(c.Network.PortMappings)
	if c.Build.Pack == PackStatic {
		c.Network.Port = staticPort
	}
	if c.Storages == nil {
		c.Storages = []Storage{}
	}
	c.normalizeProcesses()
	c.normalizeEngineExtras()
}

// Validate normalizes the config and reports every invalid field. Values end
// up in shell commands and compose files, so formats are strict.
func (c *Config) Validate() error {
	c.normalize()
	fields := apperr.Fields{}
	c.validateSource(fields)
	c.validateBuild(fields)
	c.validateNetwork(fields)
	c.validateHealth(fields)
	c.validateLimits(fields)
	c.validateStorages(fields)
	c.validateProcesses(fields)
	c.validateEngineExtras(fields)
	return fields.Err()
}

func (c *Config) validateSource(fields apperr.Fields) {
	if c.Build.Pack == PackImage {
		if !docker.ValidImage(c.Source.Image) {
			fields.Add("source.image", "Informe uma imagem válida, ex.: nginx:1.27 ou ghcr.io/org/app:tag.")
		}
		return
	}
	if !repositoryPattern.MatchString(c.Source.Repository) {
		fields.Add("source.repository", "Use uma URL https:// ou git@host:org/repo.git.")
	}
	if strings.Contains(c.Source.Repository, "://") {
		parsed, err := url.Parse(c.Source.Repository)
		if err != nil || parsed.Host == "" || parsed.Fragment != "" || parsed.RawQuery != "" {
			fields.Add("source.repository", "URL de repositório inválida. Use uma URL válida ou uma chave de deploy para autenticar.")
		}
	}
	for _, pattern := range c.Source.WatchPaths {
		if !validWatchPattern(pattern) {
			fields.Add("source.watch_paths", "Caminho de monitoramento inválido: use globs relativos ao repositório, ex.: apps/api/**.")
		}
	}
	if !branchPattern.MatchString(c.Source.Branch) || strings.Contains(c.Source.Branch, "..") {
		fields.Add("source.branch", "Nome de branch inválido.")
	}
	if c.Source.Tag != "" && !validRef(c.Source.Tag) {
		fields.Add("source.tag", "Nome de tag inválido.")
	}
	if c.Source.TagPattern != "" && !validTagPattern(c.Source.TagPattern) {
		fields.Add("source.tag_pattern", "Padrão de tag inválido: use letras, números, '.', '-', '/' e curingas, ex.: v*.")
	}
	if c.Source.Commit != "" && !commitPattern.MatchString(c.Source.Commit) {
		fields.Add("source.commit", "Informe o SHA completo do commit (40 ou 64 caracteres hexadecimais).")
	}
	usesSSH := !strings.HasPrefix(c.Source.Repository, "http")
	if usesSSH && c.Source.PrivateKeyID == "" {
		fields.Add("source.private_key_id", "Repositórios via SSH exigem uma chave de deploy.")
	}
}

func (c *Config) validateBuild(fields apperr.Fields) {
	switch c.Build.Pack {
	case PackDockerfile, PackNixpacks, PackStatic, PackImage:
	default:
		fields.Add("build.pack", "Build pack desconhecido.")
	}
	for field, value := range map[string]string{
		"build.base_directory":    c.Build.BaseDirectory,
		"build.dockerfile_path":   c.Build.DockerfilePath,
		"build.publish_directory": c.Build.PublishDirectory,
	} {
		if !safeRelativePath(value) {
			fields.Add(field, "Caminho inválido: use um caminho relativo à raiz do repositório.")
		}
	}
}

func (c *Config) validateNetwork(fields apperr.Fields) {
	if c.Network.Port < 1 || c.Network.Port > 65535 {
		fields.Add("network.port", "Porta fora do intervalo 1–65535.")
	}
	for index, domain := range c.Network.Domains {
		route, err := compose.ParseRoute(domain)
		if err != nil {
			fields.Add("network.domains", "%q: %s.", domain, err)
			continue
		}
		c.Network.Domains[index] = route.String()
	}
	for _, mapping := range c.Network.PortMappings {
		if !portMapPattern.MatchString(mapping) {
			fields.Add("network.port_mappings", "%q: use host:container, ex.: 8080:80.", mapping)
		}
	}
}

func (c *Config) validateHealth(fields apperr.Fields) {
	if !strings.HasPrefix(c.Health.Path, "/") || strings.ContainsAny(c.Health.Path, " '\"`$\\;|&") {
		fields.Add("health.path", "O caminho deve começar com / e não conter espaços ou caracteres de shell.")
	}
	if c.Health.StartPeriodSeconds < 0 || c.Health.StartPeriodSeconds > 3600 {
		fields.Add("health.start_period_seconds", "Use um valor entre 0 e 3600.")
	}
	if c.Health.Retries > 100 || c.Health.IntervalSeconds > 300 || c.Health.TimeoutSeconds > 300 {
		fields.Add("health.retries", "Tentativas (máx. 100), intervalo e timeout (máx. 300 s) fora do limite.")
	}
}

func (c *Config) validateLimits(fields apperr.Fields) {
	if c.Limits.CPUs != "" && !cpusPattern.MatchString(c.Limits.CPUs) {
		fields.Add("limits.cpus", "Use um número de CPUs, ex.: 0.5 ou 2.")
	}
	if c.Limits.Memory != "" && !memoryPattern.MatchString(c.Limits.Memory) {
		fields.Add("limits.memory", "Use um valor com unidade, ex.: 512m ou 2g.")
	}
}

func (c *Config) validateStorages(fields apperr.Fields) {
	targets := map[string]bool{}
	for index := range c.Storages {
		storage := &c.Storages[index]
		storage.Source = strings.TrimSpace(storage.Source)
		storage.Target = path.Clean(strings.TrimSpace(storage.Target))
		field := fmt.Sprintf("storages.%d", index)
		switch storage.Kind {
		case StorageVolume:
			if !volumeNamePattern.MatchString(storage.Source) {
				fields.Add(field, "Nome de volume inválido.")
			}
		case StorageBind:
			if !path.IsAbs(storage.Source) || strings.ContainsAny(storage.Source, ":\n") {
				fields.Add(field, "O diretório do host deve ser um caminho absoluto.")
			}
		default:
			fields.Add(field, "Tipo de armazenamento desconhecido.")
		}
		if !path.IsAbs(storage.Target) || storage.Target == "/" || strings.ContainsAny(storage.Target, ":\n") {
			fields.Add(field, "O destino deve ser um caminho absoluto dentro do container.")
		}
		if targets[storage.Target] {
			fields.Add(field, "Destino duplicado: %s.", storage.Target)
		}
		targets[storage.Target] = true
	}
}

// validRef accepts Git branch and tag names that are safe as data.
func validRef(ref string) bool {
	return branchPattern.MatchString(ref) && !strings.Contains(ref, "..") && !strings.Contains(ref, "//") &&
		!strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".lock")
}

func validTagPattern(pattern string) bool {
	_, err := path.Match(pattern, "")
	return err == nil && tagPatternSyntax.MatchString(pattern) && !strings.Contains(pattern, "..")
}

// safeRelativePath accepts paths that stay inside the repository.
func safeRelativePath(value string) bool {
	return !strings.ContainsAny(value, "\n\x00") && !hasParentSegment(value)
}

func hasParentSegment(value string) bool {
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return true
		}
	}
	return false
}

// cleanRelative canonicalizes a path relative to the repository root. A path
// that tries to climb out is left untouched so validation reports it instead
// of silently pointing somewhere else.
func cleanRelative(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/"
	}
	if hasParentSegment(value) {
		return value
	}
	return path.Clean("/" + value)
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

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func positiveOr(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}
