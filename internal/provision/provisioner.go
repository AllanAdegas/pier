package provision

import (
	"context"
	"errors"
	"net"
	"time"

	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/oplog"
	"pier/internal/server"
)

const (
	provisionTimeout = 45 * time.Minute
	sshPort          = "22"
	rootUser         = "root"
)

// run provisions a machine; every step is recorded in the operation and in
// the machine status. A failure leaves the machine at the provider: only
// the operator destroys it.
func (s *Service) run(machine Machine, credential Credential, provider Provider, operation *oplog.Operation) {
	ctx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	defer cancel()
	err := s.provision(ctx, machine, credential, provider, operation)
	if err != nil {
		message := localized(err)
		operation.Say("Falhou: %s", message)
		_ = s.store.setProgress(context.WithoutCancel(ctx), machine.ID, progress{status: StatusFailed, failure: message})
	}
	operation.Finish(errorText(err))
}

func (s *Service) provision(ctx context.Context, machine Machine, credential Credential, provider Provider, operation *oplog.Operation) error {
	key, err := s.credentialKey(ctx, credential)
	if err != nil {
		return err
	}
	operation.Say("Registrando a chave SSH do Pier no provedor.")
	keyRef, err := provider.EnsureKey(ctx, "pier-"+credential.ID, authorizedKeyOnly(key.PublicKey))
	if err != nil {
		return err
	}
	userData, err := cloudInit(key.PublicKey)
	if err != nil {
		return err
	}
	operation.Say("Pedindo ao provedor o servidor %s (%s, %s, %s).", machine.Name, machine.Region, machine.Size, machine.Image)
	instance, err := provider.Create(ctx, CreateRequest{Name: machine.Name, Region: machine.Region, Size: machine.Size,
		Image: machine.Image, UserData: userData, KeyRef: keyRef})
	if err != nil {
		return err
	}
	if err := s.store.setProgress(ctx, machine.ID, progress{status: StatusCreating, externalID: instance.ExternalID}); err != nil {
		return err
	}
	operation.Say("Aguardando o servidor ligar.")
	if instance, err = s.waitRunning(ctx, provider, instance); err != nil {
		return err
	}
	if err := s.store.setProgress(ctx, machine.ID, progress{status: StatusWaitingSSH, ipv4: instance.IPv4}); err != nil {
		return err
	}
	operation.Say("Servidor ligado em %s. Aguardando o SSH responder.", instance.IPv4)
	registered, err := s.register(ctx, machine, key, instance.IPv4)
	if err != nil {
		return err
	}
	return s.finish(ctx, machine, registered.ID, operation)
}

func (s *Service) register(ctx context.Context, machine Machine, key server.PrivateKey, address string) (server.Server, error) {
	if err := s.waitPort(ctx, net.JoinHostPort(address, sshPort)); err != nil {
		return server.Server{}, err
	}
	description := i18n.Instance().Sprintf("Criado pelo Pier em %s (%s, %s).", providerName(machine.Provider), machine.Region, machine.Size)
	registered, err := s.servers.Create(ctx, server.Input{Name: machine.Name, Description: description, Host: address,
		Port: 22, User: rootUser, PrivateKeyID: key.ID})
	if err != nil {
		return server.Server{}, err
	}
	if err := s.store.setProgress(ctx, machine.ID, progress{status: StatusWaitingSSH, serverID: registered.ID}); err != nil {
		return server.Server{}, err
	}
	return registered, s.waitSSH(ctx, registered.ID)
}

func (s *Service) finish(ctx context.Context, machine Machine, serverID string, operation *oplog.Operation) error {
	if machine.InstallDocker {
		if err := s.store.setProgress(ctx, machine.ID, progress{status: StatusInstalling}); err != nil {
			return err
		}
		operation.Say("Instalando o Docker pelo script oficial.")
		if _, err := s.docker.Install(ctx, serverID, server.DockerConvenience, operation.Append); err != nil {
			return err
		}
	} else if _, err := s.servers.Validate(ctx, serverID); err != nil {
		return err
	}
	operation.Say("Servidor pronto e cadastrado no Pier.")
	return s.store.setProgress(ctx, machine.ID, progress{status: StatusReady})
}

// credentialKey returns the Pier key of the credential, creating it once.
func (s *Service) credentialKey(ctx context.Context, credential Credential) (server.PrivateKey, error) {
	if credential.KeyID != "" {
		keys, err := s.keys.List(ctx)
		if err != nil {
			return server.PrivateKey{}, err
		}
		for _, key := range keys {
			if key.ID == credential.KeyID {
				return key, nil
			}
		}
	}
	key, err := s.keys.Generate(ctx, "nuvem-"+credential.Name, i18n.Instance().Sprintf("Chave dos servidores criados pela conta %s.", credential.Name))
	if err != nil {
		return server.PrivateKey{}, err
	}
	return key, s.store.setCredentialKey(ctx, credential.ID, key.ID)
}

func (s *Service) waitRunning(ctx context.Context, provider Provider, instance Instance) (Instance, error) {
	deadline := time.Now().Add(s.timing.Machine)
	for !(instance.Running && instance.IPv4 != "") {
		if time.Now().After(deadline) {
			return Instance{}, apperr.Unavailable("O provedor não ligou o servidor a tempo (estado %s).", instance.Status)
		}
		if err := sleep(ctx, s.timing.Poll); err != nil {
			return Instance{}, err
		}
		current, err := provider.Get(ctx, instance.ExternalID)
		if err != nil {
			return Instance{}, notFoundAsUnavailable(err)
		}
		instance = current
	}
	return instance, nil
}

func (s *Service) waitPort(ctx context.Context, address string) error {
	return s.retry(ctx, func() error { return s.dial(ctx, address) },
		apperr.Unavailable("A porta SSH de %s não respondeu a tempo.", address))
}

func (s *Service) waitSSH(ctx context.Context, serverID string) error {
	return s.retry(ctx, func() error { return s.servers.Probe(ctx, serverID) },
		apperr.Unavailable("O servidor não aceitou a chave do Pier a tempo. Confira o cloud-init no console do provedor."))
}

func (s *Service) retry(ctx context.Context, attempt func() error, timeout error) error {
	deadline := time.Now().Add(s.timing.SSH)
	for {
		err := attempt()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.Join(timeout, err)
		}
		if err := sleep(ctx, s.timing.Poll); err != nil {
			return err
		}
	}
}

func sleep(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func providerName(provider string) string {
	if provider == ProviderHetzner {
		return "Hetzner Cloud"
	}
	return "DigitalOcean"
}

// localized renders an error in the instance language.
func localized(err error) string {
	var appErr *apperr.Error
	if errors.As(err, &appErr) {
		return appErr.Localize(i18n.Instance())
	}
	return err.Error()
}

func errorText(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(localized(err))
}
