package cluster

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/kube"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/idgen"
	"pier/internal/platform/oplog"
	"pier/internal/platform/shell"
	"pier/internal/server"
)

// dialSucceeds reports whether a TCP connection to the address is accepted.
func dialSucceeds(ctx context.Context, address string) bool {
	dialer := net.Dialer{Timeout: portProbeTimeout}
	connection, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return false
	}
	_ = connection.Close()
	return true
}

// RecoverProvisioning marks clusters whose creation was cut short by a
// restart: nothing is working on them anymore.
func (s *Service) RecoverProvisioning(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE clusters SET status = ?, status_message = ? WHERE status = ?`,
		StatusUnreachable, "A criação foi interrompida pela reinicialização do Pier. Remova o cluster e crie de novo.",
		StatusProvisioning)
	return err
}

// CreateK3s starts a single-node K3s as a container on a server. The work
// runs in the background; follow it with Operation.
func (s *Service) CreateK3s(ctx context.Context, input K3sInput) (Cluster, error) {
	if err := input.validate(); err != nil {
		return Cluster{}, err
	}
	ex, target, err := s.connector.Executor(ctx, input.ServerID)
	if err != nil {
		return Cluster{}, err
	}
	if err := s.requireFreePorts(ctx, s.nodeAddress(target), input); err != nil {
		return Cluster{}, err
	}
	cluster := Cluster{
		ID: idgen.New(), Name: input.Name, Description: strings.TrimSpace(input.Description), Source: SourceK3s,
		ServerID: input.ServerID, K3s: K3sConfig{Image: input.Image, APIPort: input.APIPort, HTTPPort: input.HTTPPort},
		Endpoint: fmt.Sprintf("https://%s:%d", s.nodeAddress(target), input.APIPort),
		Status:   StatusProvisioning, CreatedAt: time.Now(),
	}
	if err := s.insert(ctx, cluster, ""); err != nil {
		return Cluster{}, err
	}
	operation, _ := s.operations.Begin(cluster.ID, i18n.Instance().Text("Criando cluster K3s"))
	provisionCtx, cancel := context.WithTimeout(context.Background(), provisionTimeout)
	s.mu.Lock()
	s.cancels[cluster.ID] = cancel
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			delete(s.cancels, cluster.ID)
			s.mu.Unlock()
			cancel()
		}()
		err := s.provision(provisionCtx, cluster, target, ex, operation)
		if err != nil {
			operation.Say("ERRO: %s", err)
			// Recorded with a fresh context: the provisioning one may be cancelled.
			_ = s.setStatus(context.Background(), cluster.ID, StatusUnreachable, err.Error(), "", 0)
		} else {
			operation.Say("Cluster pronto.")
		}
		operation.Finish(err)
	}()
	return cluster, nil
}

// requireFreePorts refuses ports something already answers on. Without this,
// docker may still publish the port (on another address family, for
// instance) and Pier would end up talking to that other service.
func (s *Service) requireFreePorts(ctx context.Context, host string, input K3sInput) error {
	fields := apperr.Fields{}
	for field, port := range map[string]int{"api_port": input.APIPort, "http_port": input.HTTPPort} {
		if s.portInUse(ctx, net.JoinHostPort(host, strconv.Itoa(port))) {
			fields.Add(field, "A porta %d já está em uso em %s.", port, host)
		}
	}
	return fields.Err()
}

func (s *Service) provision(ctx context.Context, cluster Cluster, target server.Server, ex executor.Executor, operation *oplog.Operation) error {
	yaml, err := k3sCompose(cluster, s.nodeAddress(target)).Marshal()
	if err != nil {
		return err
	}
	project := s.project(cluster, target)
	if err := project.Write(ctx, ex, yaml, nil); err != nil {
		return err
	}
	operation.Append("$ docker compose up --detach (" + cluster.K3s.Image + ")")
	if err := project.Up(ctx, ex, operation.Append); err != nil {
		return err
	}
	operation.Say("Aguardando o K3s gerar o kubeconfig…")
	raw, err := waitFor(ctx, kubeconfigWait, func() (string, error) {
		return ex.Run(ctx, shell.Join("docker", "exec", cluster.ContainerName(), "cat", kubeconfigPath))
	})
	if err != nil {
		return i18n.Errorf("o K3s não gerou o kubeconfig: %w", err)
	}
	kubeconfig, err := kube.RewriteServer([]byte(raw), cluster.Endpoint)
	if err != nil {
		return err
	}
	if err := s.setKubeconfig(ctx, cluster.ID, string(kubeconfig)); err != nil {
		return err
	}
	operation.Say("Aguardando o nó ficar pronto em %s…", cluster.Endpoint)
	// The status stays "provisioning" until the node registers: failed probes
	// are expected while the API server starts.
	var version string
	var nodes int
	_, err = waitFor(ctx, readinessWait, func() (string, error) {
		probeCtx, cancel := context.WithTimeout(ctx, checkTimeout)
		defer cancel()
		var err error
		version, nodes, err = s.probe(probeCtx, cluster.ID)
		if err != nil {
			s.forget(cluster.ID)
			return "", err
		}
		if nodes == 0 {
			return "", i18n.Errorf("nenhum nó registrado ainda")
		}
		return "", nil
	})
	if err != nil {
		return i18n.Errorf("o cluster não ficou pronto: %w", err)
	}
	return s.setStatus(ctx, cluster.ID, StatusReady, "", version, nodes)
}

// waitFor retries an attempt until it succeeds or the limit is reached,
// returning the last error.
func waitFor(ctx context.Context, limit time.Duration, attempt func() (string, error)) (string, error) {
	deadline := time.Now().Add(limit)
	for {
		result, err := attempt()
		if err == nil {
			return result, nil
		}
		if time.Now().After(deadline) {
			return "", err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(provisionPollEvery):
		}
	}
}

// cancelProvisioning stops a creation still in progress and waits for it to
// let go of the cluster, so removal does not race with it.
func (s *Service) cancelProvisioning(ctx context.Context, id string) error {
	s.mu.Lock()
	cancel, running := s.cancels[id]
	s.mu.Unlock()
	if !running {
		return nil
	}
	cancel()
	deadline := time.Now().Add(cancelGracePeriod)
	for s.Operation(id).Running {
		if time.Now().After(deadline) {
			return apperr.Conflict("A criação do cluster não pôde ser interrompida a tempo. Tente de novo em instantes.")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	return nil
}

func (s *Service) destroyK3s(ctx context.Context, cluster Cluster) error {
	ex, target, err := s.connector.Executor(ctx, cluster.ServerID)
	if err != nil {
		return err
	}
	containers, err := docker.ListByResource(ctx, ex, cluster.ID)
	if err != nil {
		return err
	}
	if err := docker.Remove(ctx, ex, containers); err != nil {
		return err
	}
	if _, err := ex.Run(ctx, shell.Join("docker", "volume", "rm", "--force", cluster.ID+"_data")); err != nil {
		return err
	}
	return s.project(cluster, target).RemoveDir(ctx, ex)
}
