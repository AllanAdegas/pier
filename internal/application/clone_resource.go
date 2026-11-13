package application

import (
	"context"
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// CloneOptions configure a copy of a resource into an environment (of the
// same or of another project). Domains are off by default: two resources
// answering on the same domain conflict. Secrets never cross into another
// project unless CopySecrets is set explicitly.
type CloneOptions struct {
	TargetEnvironmentID string `json:"target_environment_id"`
	Name                string `json:"name"`
	CopyVariables       bool   `json:"copy_variables"`
	CopyDomains         bool   `json:"copy_domains"`
	CopySecrets         bool   `json:"copy_secrets"`
}

// ClonePlan is what a clone may carry, decided once from the options and
// whether source and target share a project.
type ClonePlan struct {
	Options      CloneOptions
	SameProject  bool
	SameEnv      bool
	Name         string
	KeepSecrets  bool
	CopyDomains  bool
	CopyVariable bool
}

// PlanClone validates the target and decides what the clone carries. It is
// shared by applications, databases and stacks.
func PlanClone(ctx context.Context, env *envvar.Store, sourceEnvironmentID, sourceName string, options CloneOptions) (ClonePlan, error) {
	options.TargetEnvironmentID = strings.TrimSpace(options.TargetEnvironmentID)
	if options.TargetEnvironmentID == "" {
		fields := apperr.Fields{}
		fields.Add("target_environment_id", "Selecione o ambiente de destino.")
		return ClonePlan{}, fields.Err()
	}
	targetProject, err := env.ProjectOf(ctx, options.TargetEnvironmentID)
	if err != nil {
		return ClonePlan{}, err
	}
	if targetProject == "" {
		return ClonePlan{}, apperr.NotFound("Ambiente")
	}
	sourceProject, err := env.ProjectOf(ctx, sourceEnvironmentID)
	if err != nil {
		return ClonePlan{}, err
	}
	plan := ClonePlan{
		Options: options, SameProject: sourceProject == targetProject, SameEnv: sourceEnvironmentID == options.TargetEnvironmentID,
		CopyDomains: options.CopyDomains, CopyVariable: options.CopyVariables,
	}
	plan.KeepSecrets = plan.SameProject || options.CopySecrets
	plan.Name = strings.TrimSpace(options.Name)
	if plan.Name == "" {
		plan.Name = sourceName
		if plan.SameEnv {
			plan.Name = i18n.Instance().Sprintf("%s (cópia)", sourceName)
		}
	}
	return plan, nil
}

// CopyVariables copies the variables of one resource to another as the plan
// allows (see envvar.ForClone).
func (p ClonePlan) CopyVariables(ctx context.Context, env *envvar.Store, from, to envvar.Owner) error {
	if !p.CopyVariable {
		return nil
	}
	variables, err := env.List(ctx, from)
	if err != nil {
		return err
	}
	return env.ReplaceAll(ctx, to, envvar.ForClone(variables, p.KeepSecrets))
}

// CloneRepository is the repository URL a clone may keep: an embedded HTTPS
// token is a secret and is dropped when secrets are not kept.
func (p ClonePlan) CloneRepository(repository string) string {
	if p.KeepSecrets || !strings.Contains(repository, "://") {
		return repository
	}
	parsed, err := url.Parse(repository)
	if err != nil || parsed.User == nil {
		return repository
	}
	if _, hasPassword := parsed.User.Password(); parsed.Scheme == "ssh" && !hasPassword {
		return repository
	}
	parsed.User = nil
	return parsed.String()
}

// Clone copies an application's definition (never its deployments, image or
// webhook secret) into the target environment, on the same server.
func (s *Service) Clone(ctx context.Context, sourceID string, options CloneOptions) (Application, error) {
	return s.CloneWithConfigCheck(ctx, sourceID, options, nil)
}

// CloneWithConfigCheck authorizes the exact copied configuration before any
// resource is persisted, avoiding a second source read between check and copy.
func (s *Service) CloneWithConfigCheck(ctx context.Context, sourceID string, options CloneOptions, check func(Config) error) (Application, error) {
	source, err := s.Get(ctx, sourceID)
	if err != nil {
		return Application{}, err
	}
	plan, err := PlanClone(ctx, s.env, source.EnvironmentID, source.Name, options)
	if err != nil {
		return Application{}, err
	}
	config, err := s.cloneConfig(ctx, source, plan)
	if err != nil {
		return Application{}, err
	}
	if check != nil {
		if err := check(config); err != nil {
			return Application{}, err
		}
	}
	clone, err := s.Create(ctx, CreateInput{
		EnvironmentID: plan.Options.TargetEnvironmentID, ServerID: source.ServerID,
		Name: plan.Name, Description: source.Description, Config: config,
	})
	if err != nil {
		return Application{}, err
	}
	owner := func(id string) envvar.Owner { return envvar.Owner{Type: envvar.OwnerApplication, ID: id} }
	if err := plan.CopyVariables(ctx, s.env, owner(source.ID), owner(clone.ID)); err != nil {
		return Application{}, err
	}
	return clone, nil
}

func (s *Service) cloneConfig(ctx context.Context, source Application, plan ClonePlan) (Config, error) {
	var config Config
	raw, err := json.Marshal(source.Config)
	if err != nil {
		return Config{}, err
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		return Config{}, err
	}
	config.Source.Repository = plan.CloneRepository(config.Source.Repository)
	if !plan.CopyDomains {
		config.Network.Domains = []string{}
		return config, nil
	}
	automatic, err := s.store.automaticHosts(ctx, source.ID)
	if err != nil {
		return Config{}, err
	}
	config.Network.Domains = slices.DeleteFunc(config.Network.Domains, func(domain string) bool {
		for _, host := range automatic {
			if configHasHost(Config{Network: Network{Domains: []string{domain}}}, host) {
				return true
			}
		}
		return false
	})
	return config, nil
}
