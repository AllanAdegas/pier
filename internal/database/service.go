package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/notify"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/idgen"
	"pier/internal/platform/shell"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

const passwordLength = 32

// Connector reaches the server a database runs on.
type Connector interface {
	Executor(ctx context.Context, serverID string) (executor.Executor, server.Server, error)
}

// Service implements the database use cases.
type Service struct {
	mu        sync.Mutex
	active    map[string]bool
	store     *Store
	connector Connector
	events    notify.Publisher
	// onScheduleChange lets the scheduler reload when a backup policy changes.
	onScheduleChange func()
}

// NewService creates the service.
func NewService(store *Store, connector Connector, events notify.Publisher) *Service {
	return &Service{active: make(map[string]bool), store: store, connector: connector, events: events, onScheduleChange: func() {}}
}

// OnScheduleChange registers the callback fired when backup policies change.
func (s *Service) OnScheduleChange(callback func()) { s.onScheduleChange = callback }

// List returns databases, optionally of one environment.
func (s *Service) List(ctx context.Context, environmentID string) ([]Database, error) {
	return s.store.List(ctx, environmentID)
}

// Get returns one database.
func (s *Service) Get(ctx context.Context, id string) (Database, error) {
	database, err := s.store.Get(ctx, id)
	return database, notFound(err)
}

// Create provisions a database with generated credentials. It is not started.
func (s *Service) Create(ctx context.Context, input CreateInput) (Database, error) {
	fields := apperr.Fields{}
	spec, known := specFor(input.Engine)
	if !known {
		fields.Add("engine", "Engine desconhecida.")
		return Database{}, fields.Err()
	}
	now := time.Now()
	database := Database{
		ID: idgen.New(), EnvironmentID: input.EnvironmentID, ServerID: input.ServerID,
		Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description),
		Engine: input.Engine, Config: Config{Image: input.Image, Backup: BackupPolicy{Cron: "0 3 * * *"}},
		Status: docker.StatusStopped, CreatedAt: now, UpdatedAt: now,
	}
	credentials := Credentials{
		Username: orDefault(strings.TrimSpace(input.Username), defaultUsername),
		Database: orDefault(strings.TrimSpace(input.Database), defaultDatabase),
		Password: idgen.Password(passwordLength), RootPassword: idgen.Password(passwordLength),
	}
	if database.Name == "" {
		fields.Add("name", "Informe o nome do banco de dados.")
	}
	if database.EnvironmentID == "" {
		fields.Add("environment_id", "Selecione o ambiente.")
	}
	if database.ServerID == "" {
		fields.Add("server_id", "Selecione o servidor.")
	}
	if spec.UsesAccount && !identifierPattern.MatchString(credentials.Username) {
		fields.Add("username", "Use letras minúsculas, números e _ (começando por letra).")
	}
	if spec.UsesAccount && !identifierPattern.MatchString(credentials.Database) {
		fields.Add("database", "Use letras minúsculas, números e _ (começando por letra).")
	}
	database.Config.validate(spec, fields)
	if err := fields.Err(); err != nil {
		return Database{}, err
	}
	if err := s.store.insert(ctx, database, credentials); err != nil {
		return Database{}, err
	}
	return database, nil
}

// Update changes the definition; runtime changes apply on the next start.
func (s *Service) Update(ctx context.Context, id string, input UpdateInput) (Database, error) {
	release, err := s.acquire(id)
	if err != nil {
		return Database{}, err
	}
	defer release()
	if err := s.ensureIdle(ctx, id); err != nil {
		return Database{}, err
	}
	database, err := s.Get(ctx, id)
	if err != nil {
		return Database{}, err
	}
	spec, _ := specFor(database.Engine)
	fields := apperr.Fields{}
	database.Name = strings.TrimSpace(input.Name)
	database.Description = strings.TrimSpace(input.Description)
	database.Config = input.Config
	if database.Name == "" {
		fields.Add("name", "Informe o nome do banco de dados.")
	}
	database.Config.validate(spec, fields)
	if err := fields.Err(); err != nil {
		return Database{}, err
	}
	if err := s.store.update(ctx, database); err != nil {
		return Database{}, notFound(err)
	}
	s.onScheduleChange()
	return database, nil
}

// Start writes the compose project and brings the container up, applying the
// current configuration.
func (s *Service) Start(ctx context.Context, id string) error {
	release, err := s.acquire(id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.ensureIdle(ctx, id); err != nil {
		return err
	}

	database, ex, project, err := s.reach(ctx, id)
	if err != nil {
		return err
	}
	spec, _ := specFor(database.Engine)
	credentials, err := s.store.credentials(ctx, id)
	if err != nil {
		return err
	}
	yaml, err := composeFor(database, spec, credentials).Marshal()
	if err != nil {
		return err
	}
	if err := docker.EnsureNetwork(ctx, ex); err != nil {
		return unavailable(err)
	}
	if err := project.Write(ctx, ex, yaml, nil); err != nil {
		return unavailable(err)
	}
	var output strings.Builder
	sink := func(line string) { output.WriteString(line + "\n") }
	if err := project.Up(ctx, ex, sink); err != nil {
		return apperr.Unavailable("Não foi possível iniciar o banco: %s\n%s", err, tail(output.String(), 8))
	}
	return s.store.SetStatus(ctx, id, docker.StatusStarting)
}

// Stop stops the container keeping the data volume.
func (s *Service) Stop(ctx context.Context, id string) error {
	release, err := s.acquire(id)
	if err != nil {
		return err
	}
	defer release()
	if err := s.ensureIdle(ctx, id); err != nil {
		return err
	}

	_, ex, _, err := s.reach(ctx, id)
	if err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return unavailable(err)
	}
	if err := docker.Stop(ctx, ex, containers); err != nil {
		return unavailable(err)
	}
	return s.store.SetStatus(ctx, id, docker.StatusStopped)
}

// Restart recreates the container with the current configuration.
func (s *Service) Restart(ctx context.Context, id string) error {
	if err := s.Stop(ctx, id); err != nil {
		return err
	}
	return s.Start(ctx, id)
}

// Delete stops the database and erases it. deleteVolume also destroys the
// data: the volume and every backup file. Without it both are kept on the
// server. With force, an unreachable server does not block removing the record.
func (s *Service) Delete(ctx context.Context, id string, deleteVolume, force bool) error {
	release, err := s.acquire(id)
	if err != nil {
		return err
	}
	defer release()

	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	if err := s.ensureIdle(ctx, id); err != nil {
		return err
	}
	if err := s.cleanup(ctx, id, deleteVolume); err != nil && (!force || deleteVolume) {
		return apperr.Unavailable("Não foi possível limpar o servidor (%s). Use a remoção forçada para "+
			"apagar apenas o registro.", err)
	}
	if err := s.store.delete(ctx, id); err != nil {
		return notFound(err)
	}
	s.onScheduleChange()
	return nil
}

func (s *Service) cleanup(ctx context.Context, id string, deleteData bool) error {
	database, ex, project, err := s.reach(ctx, id)
	if err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	if err != nil {
		return err
	}
	if err := docker.Remove(ctx, ex, containers); err != nil {
		return err
	}
	if deleteData {
		if err := s.destroyData(ctx, ex, database); err != nil {
			return err
		}
	}
	return project.RemoveDir(ctx, ex)
}

// destroyData removes everything that holds the contents of the database:
// the data volume and the dump files. Both follow the same explicit choice,
// so no copy of the data lingers on the server after the user asked to
// destroy it, and none is lost when they did not.
func (s *Service) destroyData(ctx context.Context, ex executor.Executor, database Database) error {
	backups, err := s.store.listBackups(ctx, database.ID)
	if err != nil {
		return err
	}
	for _, b := range backups {
		if err := s.removeRemote(ctx, b, ex); err != nil {
			return fmt.Errorf("S3: %w", err)
		}
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "volume", "rm", "--force", database.ID+"_data")); err != nil {
		return fmt.Errorf("volume: %w", err)
	}
	_, target, err := s.connector.Executor(ctx, database.ServerID)
	if err != nil {
		return err
	}
	_, err = ex.Run(ctx, shell.Join("rm", "-rf", target.BackupDir(database.ID)))
	if err != nil {
		return fmt.Errorf("backups: %w", err)
	}
	return nil
}

// Connection returns credentials and URLs to reach the database.
func (s *Service) Connection(ctx context.Context, id string) (Connection, error) {
	database, err := s.Get(ctx, id)
	if err != nil {
		return Connection{}, err
	}
	credentials, err := s.store.credentials(ctx, id)
	if err != nil {
		return Connection{}, err
	}
	spec, _ := specFor(database.Engine)
	connection := Connection{
		Host: database.ID, Port: spec.Port, Credentials: credentials,
		InternalURL: spec.connectionURL(credentials, database.ID, spec.Port),
	}
	if database.Config.PublicPort != 0 {
		_, target, err := s.connector.Executor(ctx, database.ServerID)
		if err != nil {
			return Connection{}, err
		}
		host := target.Host
		if target.Kind == server.KindLocal {
			host = "localhost"
		}
		connection.PublicURL = spec.connectionURL(credentials, host, database.Config.PublicPort)
	}
	return connection, nil
}

// Containers lists the containers of the database on its server.
func (s *Service) Containers(ctx context.Context, id string) ([]docker.Container, error) {
	_, ex, _, err := s.reach(ctx, id)
	if err != nil {
		return nil, err
	}
	containers, err := docker.ListByResource(ctx, ex, id)
	return containers, unavailable(err)
}

func (s *Service) reach(ctx context.Context, id string) (Database, executor.Executor, compose.Project, error) {
	database, err := s.Get(ctx, id)
	if err != nil {
		return Database{}, nil, compose.Project{}, err
	}
	ex, target, err := s.connector.Executor(ctx, database.ServerID)
	if err != nil {
		return Database{}, nil, compose.Project{}, err
	}
	project := compose.Project{Name: id, Dir: target.ResourceDir(docker.TypeDatabase, id)}
	return database, ex, project, nil
}

func portMapping(hostPort, containerPort int) string {
	return fmt.Sprintf("%d:%d", hostPort, containerPort)
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func tail(text string, lines int) string {
	all := strings.Split(strings.TrimSpace(text), "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	return strings.Join(all, "\n")
}

func notFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Banco de dados")
	}
	return err
}

func unavailable(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	return apperr.Unavailable("Falha ao operar o servidor: %s", err)
}
