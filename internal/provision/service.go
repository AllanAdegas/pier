package provision

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"strings"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/oplog"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

// KeyIssuer creates the SSH key Pier injects into new machines.
type KeyIssuer interface {
	Generate(ctx context.Context, name, description string) (server.PrivateKey, error)
	List(ctx context.Context) ([]server.PrivateKey, error)
}

// Registrar registers, checks and removes the Pier server of a machine.
type Registrar interface {
	Create(ctx context.Context, input server.Input) (server.Server, error)
	Validate(ctx context.Context, id string) (server.Server, error)
	Probe(ctx context.Context, id string) error
	EnsureRemovable(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

// DockerInstaller installs Docker on a registered server.
type DockerInstaller interface {
	Install(ctx context.Context, id, method string, sink executor.LineSink) (server.DockerInstallResult, error)
}

// Timing bounds each wait of provisioning.
type Timing struct {
	Poll, Machine, SSH time.Duration
}

// DefaultTiming is generous: a new machine usually answers in one or two minutes.
var DefaultTiming = Timing{Poll: 5 * time.Second, Machine: 10 * time.Minute, SSH: 10 * time.Minute}

// Service manages cloud credentials and the machines created with them.
type Service struct {
	store      store
	box        *secretbox.Box
	keys       KeyIssuer
	servers    Registrar
	docker     DockerInstaller
	factories  map[string]ProviderFactory
	operations *oplog.Registry
	dial       func(ctx context.Context, address string) error
	timing     Timing
}

// NewService creates the service with the real provider APIs.
func NewService(db *sql.DB, box *secretbox.Box, keys KeyIssuer, servers Registrar, docker DockerInstaller) *Service {
	return &Service{
		store: store{db: db}, box: box, keys: keys, servers: servers, docker: docker,
		factories: map[string]ProviderFactory{
			ProviderHetzner:      func(token string) Provider { return NewHetzner(HetznerAPI, token, nil) },
			ProviderDigitalOcean: func(token string) Provider { return NewDigitalOcean(DigitalOceanAPI, token, nil) },
		},
		operations: oplog.NewRegistry(), dial: dialTCP, timing: DefaultTiming,
	}
}

// SetProviders replaces the provider clients (tests point them at fakes).
func (s *Service) SetProviders(factories map[string]ProviderFactory) { s.factories = factories }

// SetTiming changes the waits (tests make them short).
func (s *Service) SetTiming(timing Timing) { s.timing = timing }

// SetDialer replaces the TCP reachability check.
func (s *Service) SetDialer(dial func(ctx context.Context, address string) error) { s.dial = dial }

func dialTCP(ctx context.Context, address string) error {
	connection, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp", address)
	if err == nil {
		connection.Close()
	}
	return err
}

// Credentials lists the cloud accounts, never their tokens.
func (s *Service) Credentials(ctx context.Context) ([]Credential, error) {
	return s.store.credentials(ctx)
}

// CreateCredential checks the token against the provider, then stores it sealed.
func (s *Service) CreateCredential(ctx context.Context, input CredentialInput) (Credential, error) {
	input, err := input.validate()
	if err != nil {
		return Credential{}, err
	}
	if _, err := s.factories[input.Provider](input.Token).Catalog(ctx); err != nil {
		return Credential{}, err
	}
	sealed, err := s.box.Seal(input.Token)
	if err != nil {
		return Credential{}, err
	}
	credential := Credential{ID: idgen.New(), Provider: input.Provider, Name: input.Name, CreatedAt: time.Now()}
	if err := s.store.insertCredential(ctx, credential, sealed); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Credential{}, apperr.Conflict("Já existe uma conta com este nome.")
		}
		return Credential{}, err
	}
	return credential, nil
}

// DeleteCredential forgets a cloud account no machine depends on. It does
// not touch anything at the provider.
func (s *Service) DeleteCredential(ctx context.Context, id string) error {
	count, err := s.store.countMachines(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return apperr.Conflict("Remova antes os %d servidor(es) criados com esta conta.", count)
	}
	return credentialNotFound(s.store.deleteCredential(ctx, id))
}

// Catalog lists regions, sizes and images of a cloud account.
func (s *Service) Catalog(ctx context.Context, credentialID string) (Catalog, error) {
	_, provider, err := s.provider(ctx, credentialID)
	if err != nil {
		return Catalog{}, err
	}
	return provider.Catalog(ctx)
}

func (s *Service) provider(ctx context.Context, credentialID string) (Credential, Provider, error) {
	credential, sealed, err := s.store.credential(ctx, credentialID)
	if err != nil {
		return Credential{}, nil, credentialNotFound(err)
	}
	token, err := s.box.Open(sealed)
	if err != nil {
		return Credential{}, nil, err
	}
	factory, known := s.factories[credential.Provider]
	if !known {
		return Credential{}, nil, apperr.Invalid("Provedor desconhecido.")
	}
	return credential, factory(token), nil
}

// Machines lists the machines Pier created.
func (s *Service) Machines(ctx context.Context) ([]Machine, error) { return s.store.machines(ctx) }

// MachineWithProgress is a machine and the log of its provisioning.
type MachineWithProgress struct {
	Machine
	Operation oplog.Snapshot `json:"operation"`
}

// Machine returns one machine with its provisioning log.
func (s *Service) Machine(ctx context.Context, id string) (MachineWithProgress, error) {
	// The log first: a finished log then never pairs with an older status.
	snapshot, _ := s.operations.Get(id)
	machine, err := s.store.machine(ctx, id)
	if err != nil {
		return MachineWithProgress{}, machineNotFound(err)
	}
	return MachineWithProgress{Machine: machine, Operation: snapshot}, nil
}

// CreateMachine asks the provider for a machine and provisions it in the
// background: wait for it to run, wait for SSH, register it, install Docker.
func (s *Service) CreateMachine(ctx context.Context, input MachineInput) (Machine, error) {
	input, err := input.validate()
	if err != nil {
		return Machine{}, err
	}
	credential, provider, err := s.provider(ctx, input.CredentialID)
	if err != nil {
		return Machine{}, err
	}
	taken, err := s.store.machineNameTaken(ctx, input.Name)
	if err != nil {
		return Machine{}, err
	}
	if taken {
		return Machine{}, apperr.Conflict("Já existe um servidor de nuvem com este nome.")
	}
	machine := Machine{ID: idgen.New(), CredentialID: credential.ID, Provider: credential.Provider, Name: input.Name,
		Region: input.Region, Size: input.Size, Image: input.Image, InstallDocker: input.InstallDocker,
		Status: StatusCreating, CreatedAt: time.Now()}
	if err := s.store.insertMachine(ctx, machine); err != nil {
		return Machine{}, err
	}
	operation, _ := s.operations.Begin(machine.ID, i18n.Instance().Sprintf("Criando %s", machine.Name))
	go s.run(machine, credential, provider, operation)
	return machine, nil
}

// Destroy deletes the machine at the provider and its Pier server. It runs
// only on an explicit request with the machine name typed, never on failure.
func (s *Service) Destroy(ctx context.Context, id, confirmation string) error {
	machine, err := s.store.machine(ctx, id)
	if err != nil {
		return machineNotFound(err)
	}
	if strings.TrimSpace(confirmation) != machine.Name {
		return apperr.Invalid("Digite o nome do servidor para confirmar a destruição.")
	}
	if snapshot, _ := s.operations.Get(id); snapshot.Running {
		return apperr.Conflict("Aguarde o fim da criação antes de destruir o servidor.")
	}
	if machine.ServerID != "" {
		if err := s.servers.EnsureRemovable(ctx, machine.ServerID); err != nil {
			return err
		}
	}
	if machine.ExternalID != "" {
		_, provider, err := s.provider(ctx, machine.CredentialID)
		if err != nil {
			return err
		}
		if err := provider.Delete(ctx, machine.ExternalID); err != nil {
			return err
		}
	}
	if machine.ServerID != "" {
		if err := s.servers.Delete(ctx, machine.ServerID); err != nil && !isNotFound(err) {
			return err
		}
	}
	s.operations.Forget(id)
	return s.store.deleteMachine(ctx, id)
}

// Recover marks provisioning interrupted by a restart as failed; the
// machine may exist at the provider and is left for the operator to decide.
func (s *Service) Recover(ctx context.Context) error {
	return s.store.interrupt(ctx, i18n.Instance().Text("O Pier reiniciou durante a criação. Confira o servidor no provedor e destrua-o se não for usá-lo."))
}

func credentialNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Conta do provedor")
	}
	return err
}

func machineNotFound(err error) error {
	if errors.Is(err, sqlite.ErrNotFound) {
		return apperr.NotFound("Servidor de nuvem")
	}
	return err
}

func isNotFound(err error) bool {
	appErr, ok := apperr.As(err)
	return ok && appErr.Kind == apperr.KindNotFound
}
