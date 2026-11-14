package provision

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/secretbox/testbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type fakeProvider struct {
	mu        sync.Mutex
	token     string
	created   []CreateRequest
	deleted   []string
	polls     int
	createErr error
}

func (p *fakeProvider) Catalog(context.Context) (Catalog, error) {
	if p.token != testToken {
		return Catalog{}, apperr.Invalid("O provedor recusou o token. Confira se ele existe e tem permissão de leitura e escrita.")
	}
	return Catalog{Regions: []Option{{ID: "fsn1"}}}, nil
}
func (p *fakeProvider) EnsureKey(context.Context, string, string) (string, error) { return "7", nil }
func (p *fakeProvider) Create(_ context.Context, request CreateRequest) (Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.createErr != nil {
		return Instance{}, p.createErr
	}
	p.created = append(p.created, request)
	return Instance{ExternalID: "99", Status: "initializing"}, nil
}
func (p *fakeProvider) Get(context.Context, string) (Instance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.polls++
	if p.polls < 2 {
		return Instance{ExternalID: "99", Status: "starting"}, nil
	}
	return Instance{ExternalID: "99", Status: "running", Running: true, IPv4: "203.0.113.9"}, nil
}
func (p *fakeProvider) Delete(_ context.Context, id string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleted = append(p.deleted, id)
	return nil
}

type fakeKeys struct{ generated int }

func (k *fakeKeys) Generate(context.Context, string, string) (server.PrivateKey, error) {
	k.generated++
	return server.PrivateKey{ID: "key-1", PublicKey: testKey}, nil
}
func (k *fakeKeys) List(context.Context) ([]server.PrivateKey, error) {
	return []server.PrivateKey{{ID: "key-1", PublicKey: testKey}}, nil
}

type fakeRegistrar struct {
	db        *sql.DB
	mu        sync.Mutex
	inputs    []server.Input
	probes    int
	removable error
	deleted   []string
}

func (r *fakeRegistrar) Create(ctx context.Context, input server.Input) (server.Server, error) {
	r.mu.Lock()
	r.inputs = append(r.inputs, input)
	r.mu.Unlock()
	_, err := r.db.ExecContext(ctx, `INSERT INTO servers(id,name,kind,data_dir,created_at) VALUES('srv',?,'ssh','/data/pier',?)`, input.Name, sqlite.FormatTime(time.Now()))
	return server.Server{ID: "srv", Name: input.Name}, err
}
func (r *fakeRegistrar) Validate(context.Context, string) (server.Server, error) {
	return server.Server{}, nil
}
func (r *fakeRegistrar) Probe(context.Context, string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probes++
	if r.probes < 2 {
		return errors.New("ssh: handshake failed")
	}
	return nil
}
func (r *fakeRegistrar) EnsureRemovable(context.Context, string) error { return r.removable }
func (r *fakeRegistrar) Delete(_ context.Context, id string) error {
	r.deleted = append(r.deleted, id)
	return nil
}

type fakeInstaller struct{ methods []string }

func (f *fakeInstaller) Install(_ context.Context, id, method string, sink executor.LineSink) (server.DockerInstallResult, error) {
	f.methods = append(f.methods, id+":"+method)
	sink("installed")
	return server.DockerInstallResult{DockerVersion: "27.3.1"}, nil
}

type fixture struct {
	service   *Service
	provider  *fakeProvider
	registrar *fakeRegistrar
	installer *fakeInstaller
	keys      *fakeKeys
	db        *sql.DB
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "pier.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	box := testbox.Box(t)
	f := fixture{provider: &fakeProvider{}, registrar: &fakeRegistrar{db: db}, installer: &fakeInstaller{}, keys: &fakeKeys{}, db: db}
	f.service = NewService(db, box, f.keys, f.registrar, f.installer)
	f.service.SetProviders(map[string]ProviderFactory{ProviderHetzner: func(token string) Provider { f.provider.token = token; return f.provider }})
	f.service.SetTiming(Timing{Poll: time.Millisecond, Machine: 2 * time.Second, SSH: 2 * time.Second})
	f.service.SetDialer(func(context.Context, string) error { return nil })
	return f
}

func waitMachine(t *testing.T, s *Service, id string) MachineWithProgress {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		machine, err := s.Machine(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !machine.Operation.Running {
			return machine
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("provisioning did not finish")
	return MachineWithProgress{}
}

func TestCredentialTokenIsVerifiedSealedAndNeverListed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "x", Token: "wrong-token-0123456789"}); err == nil {
		t.Fatal("rejected token stored")
	}
	if _, err := f.service.CreateCredential(ctx, CredentialInput{Provider: "aws", Name: "x", Token: testToken}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	credential, err := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: " " + testToken + " "})
	if err != nil {
		t.Fatal(err)
	}
	var stored string
	if err := f.db.QueryRow(`SELECT token FROM cloud_credentials WHERE id = ?`, credential.ID).Scan(&stored); err != nil || strings.Contains(stored, testToken) {
		t.Fatalf("token stored in clear: %q %v", stored, err)
	}
	if _, err := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: testToken}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if catalog, err := f.service.Catalog(ctx, credential.ID); err != nil || len(catalog.Regions) != 1 {
		t.Fatalf("catalog with unsealed token %+v %v", catalog, err)
	}
}

func TestCreateMachineProvisionsRegistersAndInstallsDocker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	credential, err := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: testToken})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "Web_1", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04"}); err == nil {
		t.Fatal("invalid hostname accepted")
	}
	machine, err := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "web-1", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04", InstallDocker: true})
	if err != nil {
		t.Fatal(err)
	}
	done := waitMachine(t, f.service, machine.ID)
	if done.Status != StatusReady || done.ServerID != "srv" || done.IPv4 != "203.0.113.9" || done.ExternalID != "99" || done.Operation.Error != "" {
		t.Fatalf("machine %+v", done)
	}
	if len(f.provider.created) != 1 || !strings.Contains(f.provider.created[0].UserData, authorizedKeyOnly(testKey)) || f.provider.created[0].KeyRef != "7" {
		t.Fatalf("create request %+v", f.provider.created)
	}
	input := f.registrar.inputs[0]
	if input.Host != "203.0.113.9" || input.User != "root" || input.Port != 22 || input.PrivateKeyID != "key-1" {
		t.Fatalf("registered %+v", input)
	}
	if strings.Join(f.installer.methods, ",") != "srv:convenience" || f.keys.generated != 1 {
		t.Fatalf("installer %v keys %d", f.installer.methods, f.keys.generated)
	}
	if _, err := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "web-1", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04"}); err == nil {
		t.Fatal("duplicate name accepted")
	}
	if err := f.service.DeleteCredential(ctx, credential.ID); err == nil {
		t.Fatal("credential with machines deleted")
	}
}

func TestFailedProvisioningIsNeverDestroyedAutomatically(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	credential, _ := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: testToken})
	f.service.SetDialer(func(context.Context, string) error { return errors.New("connection refused") })
	f.service.SetTiming(Timing{Poll: time.Millisecond, Machine: time.Second, SSH: 20 * time.Millisecond})
	machine, err := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "web-2", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04"})
	if err != nil {
		t.Fatal(err)
	}
	done := waitMachine(t, f.service, machine.ID)
	if done.Status != StatusFailed || !strings.Contains(done.Error, "SSH") || len(f.provider.deleted) != 0 {
		t.Fatalf("failed machine %+v deleted %v", done, f.provider.deleted)
	}
}

func TestDestroyRequiresTypedNameAndAnEmptyServer(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	credential, _ := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: testToken})
	machine, _ := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "web-3", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04"})
	waitMachine(t, f.service, machine.ID)
	if err := f.service.Destroy(ctx, machine.ID, "web"); err == nil || len(f.provider.deleted) != 0 {
		t.Fatal("wrong confirmation destroyed")
	}
	f.registrar.removable = apperr.Conflict("O servidor ainda hospeda %d recurso(s). Remova-os antes.", 1)
	if err := f.service.Destroy(ctx, machine.ID, "web-3"); err == nil || len(f.provider.deleted) != 0 {
		t.Fatal("server with resources destroyed")
	}
	f.registrar.removable = nil
	if err := f.service.Destroy(ctx, machine.ID, "web-3"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(f.provider.deleted, ",") != "99" || strings.Join(f.registrar.deleted, ",") != "srv" {
		t.Fatalf("deleted %v %v", f.provider.deleted, f.registrar.deleted)
	}
	if _, err := f.service.Machine(ctx, machine.ID); err == nil {
		t.Fatal("record kept")
	}
	if err := f.service.DeleteCredential(ctx, credential.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRecoverMarksInterruptedProvisioning(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	credential, _ := f.service.CreateCredential(ctx, CredentialInput{Provider: ProviderHetzner, Name: "Conta", Token: testToken})
	f.provider.createErr = apperr.Unavailable("O provedor limitou as requisições. Tente de novo em instantes.")
	machine, _ := f.service.CreateMachine(ctx, MachineInput{CredentialID: credential.ID, Name: "web-4", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04"})
	waitMachine(t, f.service, machine.ID)
	if _, err := f.db.Exec(`UPDATE cloud_machines SET status = 'installing' WHERE id = ?`, machine.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	recovered, _ := f.service.Machine(ctx, machine.ID)
	if recovered.Status != StatusFailed || !strings.Contains(recovered.Error, "reiniciou") {
		t.Fatalf("%+v", recovered)
	}
}
