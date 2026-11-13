package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"pier/internal/application"
 "pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/envvar"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/oplog"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

// Connector reaches the server a stack runs on.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Service implements the stack use cases.
type Service struct {
	db         *sql.DB
	env        *envvar.Store
	connector  Connector
	operations *oplog.Registry
	guard      DeployGuard
	domains    DomainChecker
	git        gitStore
	keys       KeyVault
 gitProvider application.GitCredentials
}

// DeployGuard decides whether an environment accepts deployments now (a
// frozen environment refuses them).
type DeployGuard interface {
	CheckDeploy(ctx context.Context, environmentID string) error
}

// SetDeployGuard installs the guard consulted before every deployment.
func (s *Service) SetDeployGuard(guard DeployGuard) { s.guard = guard }

// NewService creates the service.
func NewService(db *sql.DB, env *envvar.Store, connector Connector, operations *oplog.Registry) *Service {
	return &Service{db: db, env: env, connector: connector, operations: operations}
}

const stackColumns = `id, environment_id, server_id, name, description, template, compose, directory, compose_file,
	project_name, compose_project, routes, status, created_at, updated_at`

// List returns stacks, optionally of one environment.
func (s *Service) List(ctx context.Context, environmentID string) ([]Stack, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+stackColumns+` FROM stacks
		WHERE (? = '' OR environment_id = ?) ORDER BY created_at`, environmentID, environmentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	stacks := []Stack{}
	for rows.Next() {
		stack, err := scanStack(rows)
		if err != nil {
			return nil, err
		}
		stacks = append(stacks, stack)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	sources, err := s.git.loadAll(ctx, environmentID)
	for index := range stacks {
		stacks[index].Git = sources[stacks[index].ID]
	}
	return stacks, err
}

// Get returns one stack.
func (s *Service) Get(ctx context.Context, id string) (Stack, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+stackColumns+` FROM stacks WHERE id = ?`, id)
	stack, err := scanStack(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Stack{}, apperr.NotFound("Stack")
	}
	if err != nil {
		return Stack{}, err
	}
	return stack, s.attachGit(ctx, &stack)
}

// SetStatus records the runtime status observed on the server.
func (s *Service) SetStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE stacks SET status = ? WHERE id = ? AND status <> ?`, status, id, status)
	return err
}

// Create registers a stack from a catalog template, a compose file or a
// folder of the server that holds one.
func (s *Service) Create(ctx context.Context, input CreateInput) (Stack, error) {
	now := time.Now()
	stack := Stack{
		ID: idgen.New(), EnvironmentID: input.EnvironmentID, ServerID: input.ServerID,
		Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		Template: input.Template, Compose: input.Compose, Routes: input.Routes,
		Directory: strings.TrimSpace(input.Directory), ComposeFile: strings.TrimSpace(input.ComposeFile),
		ProjectName: strings.TrimSpace(input.ProjectName),
		Status:      docker.StatusStopped, CreatedAt: now, UpdatedAt: now,
	}
	fields := apperr.Fields{}
	if input.Git != nil {
		if err := s.git.available(); err != nil {
			return Stack{}, err
		}
		source := s.newGitSource(ctx, *input.Git, fields)
		stack.Git = &source
	}
	if stack.EnvironmentID == "" {
		fields.Add("environment_id", "Selecione o ambiente.")
	}
	if stack.ServerID == "" {
		fields.Add("server_id", "Selecione o servidor.")
	}
	raw, resolved, err := s.resolveSource(ctx, &stack, fields)
	if err != nil {
		return Stack{}, err
	}
	if err := s.checkProjectName(ctx, stack, fields); err != nil {
		return Stack{}, err
	}
	for key := range input.Env {
		if !envvar.ValidKey(key) {
			fields.Add("env", "Nome de variável inválido: %q.", key)
		}
	}
	if err := validate(&stack, raw, resolved, fields); err != nil {
		return Stack{}, err
	}
	if stack.ProjectName == "" {
		stack.ComposeProject = docker.ReadableName(stack.Name, stack.ID)
	}
	routes, err := json.Marshal(stack.Routes)
	if err != nil {
		return Stack{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO stacks
		(id, environment_id, server_id, name, description, template, compose, directory, compose_file, project_name,
		 compose_project, routes, status, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		stack.ID, stack.EnvironmentID, stack.ServerID, stack.Name, stack.Description, stack.Template, stack.Compose,
		stack.Directory, stack.ComposeFile, stack.ProjectName, stack.ComposeProject, string(routes), stack.Status,
		sqlite.FormatTime(now), sqlite.FormatTime(now))
	if err != nil {
		return Stack{}, err
	}
	if err := s.saveNewGitSource(ctx, stack); err != nil {
		return Stack{}, err
	}
	if err := s.env.SetMissing(ctx, owner(stack.ID), input.Env); err != nil {
		return Stack{}, err
	}
	return stack, s.env.SetMissing(ctx, owner(stack.ID), generatedDefaults(raw, input.Env))
}

// resolveSource finds the compose file of a stack being created. resolved is
// false when the source has a problem already reported on fields.
func (s *Service) resolveSource(ctx context.Context, stack *Stack, fields apperr.Fields) (raw string, resolved bool, err error) {
	if stack.FromGit() {
		return s.resolveGitSource(ctx, stack, fields)
	}
	if stack.FromDirectory() {
		if stack.Template != "" || stack.Compose != "" {
			fields.Add("directory", "Escolha uma única origem: catálogo, compose ou pasta.")
			return "", false, nil
		}
		return s.directorySource(ctx, stack, fields)
	}
	if stack.ProjectName != "" || stack.ComposeFile != "" {
		fields.Add("directory", "Arquivo compose e nome de projeto só se aplicam a uma stack de pasta.")
		return "", false, nil
	}
	if stack.Template != "" {
		template, found := FindTemplate(stack.Template)
		if !found {
			fields.Add("template", "Template desconhecido: %s.", stack.Template)
			return "", false, nil
		}
		stack.Compose = template.Compose
	}
	return stack.Compose, true, nil
}

// checkProjectName keeps compose project names unique: two stacks sharing
// one would overwrite each other's containers.
func (s *Service) checkProjectName(ctx context.Context, stack Stack, fields apperr.Fields) error {
	if stack.ProjectName == "" {
		return nil
	}
	if !projectNamePattern.MatchString(stack.ProjectName) {
		fields.Add("project_name", "Use letras minúsculas, números, '-' e '_', começando por letra ou número.")
		return nil
	}
	var taken int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM stacks
		WHERE id <> ? AND (project_name = ? OR compose_project = ? OR id = ?)`,
		stack.ID, stack.ProjectName, stack.ProjectName, stack.ProjectName).Scan(&taken)
	if err != nil {
		return err
	}
	if taken > 0 {
		fields.Add("project_name", "Outra stack já usa este nome de projeto.")
	}
	return nil
}

// Update changes the definition; it takes effect on the next deployment.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Stack, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return Stack{}, err
	}
	stack.Name = strings.TrimSpace(input.Name)
	stack.Description = strings.TrimSpace(input.Description)
	stack.Routes = input.Routes
	fields := apperr.Fields{}
	raw, resolved := input.Compose, true
	if stack.FromGit() {
		if raw, resolved, err = s.updateGitSource(ctx, &stack, input.Git, fields); err != nil {
			return Stack{}, err
		}
	} else if stack.FromDirectory() {
		if directory := strings.TrimSpace(input.Directory); directory != "" {
			stack.Directory = directory
		}
		if file := strings.TrimSpace(input.ComposeFile); file != "" {
			stack.ComposeFile = file
		}
		if raw, resolved, err = s.directorySource(ctx, &stack, fields); err != nil {
			return Stack{}, err
		}
	} else {
		stack.Compose = input.Compose
	}
	if err := validate(&stack, raw, resolved, fields); err != nil {
		return Stack{}, err
	}
	routes, err := json.Marshal(stack.Routes)
	if err != nil {
		return Stack{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE stacks SET name = ?, description = ?, compose = ?, directory = ?,
		compose_file = ?, routes = ?, updated_at = ? WHERE id = ?`, stack.Name, stack.Description, stack.Compose,
		stack.Directory, stack.ComposeFile, string(routes), sqlite.FormatTime(time.Now()), id)
	if err != nil {
		return Stack{}, err
	}
	if err := s.saveGitSource(ctx, stack); err != nil {
		return Stack{}, err
	}
	return stack, s.generate(ctx, id, raw)
}

// generate creates the generated variables a compose file references and
// that have no value yet.
func (s *Service) generate(ctx context.Context, id, raw string) error {
	existing, err := s.env.Map(ctx, owner(id))
	if err != nil {
		return err
	}
	return s.env.SetMissing(ctx, owner(id), generatedDefaults(raw, existing))
}

// validate checks the definition of a stack against its compose file.
// resolved is false when the file could not be obtained and the reason is
// already in fields.
func validate(stack *Stack, raw string, resolved bool, fields apperr.Fields) error {
	if stack.Name == "" {
		fields.Add("name", "Informe o nome da stack.")
	}
	if stack.Routes == nil {
		stack.Routes = []Route{}
	}
	if !resolved {
		if stack.FromGit() {
			checkRoutes(stack, nil, fields)
		}
		return fields.Err()
	}
	doc, err := parseCompose(raw)
	if err != nil {
		appErr, _ := apperr.As(err)
		field := "compose"
		if stack.FromDirectory() {
			field = "compose_file"
		}
		fields.Set(field, appErr.Text())
		return fields.Err()
	}
	checkRoutes(stack, doc.serviceNames(), fields)
	return fields.Err()
}

// checkRoutes validates the routes of a stack and canonicalizes their
// domains. services lists what the compose file declares; nil skips the check
// that a routed service exists (the file is not available yet).
func checkRoutes(stack *Stack, services []string, fields apperr.Fields) {
	for index := range stack.Routes {
		route := &stack.Routes[index]
		field := fmt.Sprintf("routes.%d", index)
		if services != nil && !slices.Contains(services, route.Service) {
			fields.Add(field, "O serviço %q não existe no compose.", route.Service)
		}
		if route.Port < 1 || route.Port > 65535 {
			fields.Add(field, "Porta fora do intervalo 1–65535.")
		}
		parsed, err := compose.ParseRoute(route.Domain)
		if err != nil {
			fields.Add(field, "%q: %s.", route.Domain, err)
			continue
		}
		route.Domain = parsed.String()
	}
}

// SourceView is the compose file of a stack as it is right now.
type SourceView struct {
	Directory   string `json:"directory"`
	ComposeFile string `json:"compose_file"`
	Content     string `json:"content"`
	// EnvFile tells whether the folder has a .env that docker compose reads.
	EnvFile bool `json:"env_file"`
}

// Source returns the compose file a stack runs. For a directory stack it is
// read from the server at that moment.
func (s *Service) Source(ctx context.Context, id string) (SourceView, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return SourceView{}, err
	}
	if !stack.FromDirectory() {
		return SourceView{Content: stack.Compose}, nil
	}
	ex, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return SourceView{}, err
	}
	project := projectFor(ctx, ex, target, stack)
	content, err := readSource(ctx, ex, project.BaseFile)
	if err != nil && stack.FromGit() {
		return SourceView{}, errNotCloned
	}
	if err != nil {
		return SourceView{}, apperr.Unavailable("Não foi possível ler %s: %s", project.BaseFile, lastLine(err))
	}
	return SourceView{
		Directory: stack.Directory, ComposeFile: stack.ComposeFile, Content: content, EnvFile: len(project.EnvFiles) > 0,
	}, nil
}

// document returns the parsed compose file of a stack.
func (s *Service) document(ctx context.Context, id string) (Stack, string, document, error) {
	stack, err := s.Get(ctx, id)
	if err != nil {
		return Stack{}, "", nil, err
	}
	raw := stack.Compose
	if stack.FromDirectory() {
		view, err := s.Source(ctx, id)
		if err != nil {
			return Stack{}, "", nil, err
		}
		raw = view.Content
	}
	doc, err := parseCompose(raw)
	return stack, raw, doc, err
}

// Services lists the service names declared in the compose of a stack.
func (s *Service) Services(ctx context.Context, id string) ([]string, error) {
	_, _, doc, err := s.document(ctx, id)
	if errors.Is(err, errNotCloned) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	return doc.serviceNames(), nil
}

// Where the value of a variable comes from.
const (
	// ValueStored is a value kept by Pier (defined by the user or generated).
	ValueStored = "pier"
	// ValueFolder is a value in the .env of the folder of a directory stack.
	ValueFolder = "folder"
	// ValueAddress is derived from the domain of a published service.
	ValueAddress = "address"
)

// VariableStatus is a variable the compose file of a stack interpolates, and
// whether it will have a value on the next deployment.
type VariableStatus struct {
	Variable
	// Set is true when a value will be there: stored, generated, derived
	// from a domain or present in the .env of the folder.
	Set bool `json:"set"`
	// Source tells where the value comes from; empty when there is none.
	Source string `json:"source"`
}

// WithoutSecrets drops the default the compose file gives the variable: a
// fallback such as ${DB_PASSWORD:-s3cret} is a credential.
func (v VariableStatus) WithoutSecrets() VariableStatus {
	v.Default = ""
	return v
}

// Variables lists what the compose file expects, so missing values are seen
// before a deployment fails on them.
func (s *Service) Variables(ctx context.Context, id string) ([]VariableStatus, error) {
	stack, _, doc, err := s.document(ctx, id)
	if errors.Is(err, errNotCloned) {
		return []VariableStatus{}, nil
	}
	if err != nil {
		return nil, err
	}
	stored, err := s.env.Map(ctx, owner(id))
	if err != nil {
		return nil, err
	}
	addresses := routeVariables(stack.Routes)
	inFolder := s.folderKeys(ctx, stack)
	statuses := []VariableStatus{}
	for _, variable := range doc.referencedVariables() {
		status := VariableStatus{Variable: variable}
		// Same precedence as the deployment: an address or a value kept by
		// Pier wins over the .env of the folder.
		if _, isAddress := addresses[variable.Name]; isAddress {
			status.Source = ValueAddress
		} else if _, isStored := stored[variable.Name]; isStored || variable.Name == directoryVariable {
			status.Source = ValueStored
		} else if inFolder[variable.Name] {
			status.Source = ValueFolder
		}
		status.Set = status.Source != ""
		statuses = append(statuses, status)
	}
	return statuses, nil
}

var dotenvKey = regexp.MustCompile(`(?m)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=`)

// folderKeys returns the names defined in the .env of a directory stack.
// Only the names: the values are read and dropped right here, and never
// leave this function.
func (s *Service) folderKeys(ctx context.Context, stack Stack) map[string]bool {
	keys := map[string]bool{}
	if !stack.FromDirectory() {
		return keys
	}
	ex, target, err := s.connector.Executor(ctx, stack.ServerID)
	if err != nil {
		return keys
	}
	for _, file := range projectFor(ctx, ex, target, stack).EnvFiles {
		content, err := readSource(ctx, ex, file)
		if err != nil {
			continue
		}
		for _, match := range dotenvKey.FindAllStringSubmatch(content, -1) {
			keys[match[1]] = true
		}
	}
	return keys
}

func owner(stackID string) envvar.Owner { return envvar.Owner{Type: envvar.OwnerStack, ID: stackID} }

type rowScanner interface {
	Scan(dest ...any) error
}

func scanStack(row rowScanner) (Stack, error) {
	var stack Stack
	var routes, createdAt, updatedAt string
	err := row.Scan(&stack.ID, &stack.EnvironmentID, &stack.ServerID, &stack.Name, &stack.Description, &stack.Template,
		&stack.Compose, &stack.Directory, &stack.ComposeFile, &stack.ProjectName, &stack.ComposeProject, &routes, &stack.Status,
		&createdAt, &updatedAt)
	if err != nil {
		return Stack{}, err
	}
	if err := json.Unmarshal([]byte(routes), &stack.Routes); err != nil {
		return Stack{}, fmt.Errorf("decode routes of stack %s: %w", stack.ID, err)
	}
	stack.CreatedAt = sqlite.ParseTime(createdAt)
	stack.UpdatedAt = sqlite.ParseTime(updatedAt)
	return stack, nil
}
