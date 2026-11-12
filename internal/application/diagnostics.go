package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"pier/internal/diagnose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

func (s *Store) setDeploymentDetails(ctx context.Context, deployment Deployment) error {
	if deployment.Hints == nil {
		deployment.Hints = []diagnose.Hint{}
	}
	if deployment.RetiredLogs == nil {
		deployment.RetiredLogs = []RetiredLog{}
	}
	hints, err := json.Marshal(deployment.Hints)
	if err != nil {
		return err
	}
	logs, err := json.Marshal(deployment.RetiredLogs)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE deployments SET hints=?, retired_logs=?, suggested_port=? WHERE id=?`, string(hints), string(logs), deployment.SuggestedPort, deployment.ID)
	return err
}

func (j *job) diagnoseListeners(ctx context.Context, container string) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	output, _ := j.ex.Run(ctx, shell.Join("docker", "exec", container, "cat", "/proc/net/tcp", "/proc/net/tcp6"))
	listeners := diagnose.ParseProcNet(output)
	hints := diagnose.ExplainListeners(j.app.Config.Network.Port, listeners, string(i18n.Instance()))
	if len(hints) == 0 {
		return
	}
	j.deployment.Hints = append(j.deployment.Hints, hints...)
	if hints[0].Code == "listener_port_mismatch" && len(listeners) > 0 {
		j.deployment.SuggestedPort = listeners[0].Port
		for _, listener := range listeners {
			if !listener.Loopback {
				j.deployment.SuggestedPort = listener.Port
				break
			}
		}
	}
	_ = j.log.store.setDeploymentDetails(context.WithoutCancel(ctx), j.deployment)
}
func (d *Deployer) explainFailure(ctx context.Context, id string, log *deployLog, failure error) {
	// Read backwards with a fixed bound; do not load an entire build to explain it.
	rows, err := d.store.db.QueryContext(ctx, `SELECT line FROM deployment_logs WHERE deployment_id=? ORDER BY id DESC LIMIT 80`, id)
	if err != nil {
		return
	}
	lines := []string{}
	for rows.Next() {
		var line string
		if rows.Scan(&line) == nil {
			lines = append(lines, line)
		}
	}
	rows.Close()
	text := strings.Join(lines, "\n") + "\n" + log.mask(describeFailure(failure))
	code := 0
	var exit *executor.ExitError
	if errors.As(failure, &exit) {
		code = exit.Code
	}
	hints := diagnose.Explain(text, code)
	if len(hints) == 0 {
		return
	}
	deployment, err := d.store.GetDeployment(ctx, id)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, hint := range deployment.Hints {
		seen[hint.Code] = true
	}
	for _, hint := range hints {
		if !seen[hint.Code] {
			hint = hint.Localize(i18n.Instance())
			hint.Title = log.mask(hint.Title)
			hint.Fix = log.mask(hint.Fix)
			deployment.Hints = append(deployment.Hints, hint)
			seen[hint.Code] = true
		}
	}
	_ = d.store.setDeploymentDetails(ctx, deployment)
}

// ImagePorts inspects a ready image, pulling it when necessary after the
// operator explicitly requests suggestions in the creation form.
func (s *Service) ImagePorts(ctx context.Context, serverID, image string) ([]int, error) {
	image = strings.TrimSpace(image)
	if !docker.ValidImage(image) {
		return nil, apperr.Invalid("Informe uma imagem válida, ex.: nginx:1.27 ou ghcr.io/org/app:tag.")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	ex, _, err := s.connector.Executor(ctx, serverID)
	if err != nil {
		return nil, err
	}
	output, err := ex.Run(ctx, shell.Join("docker", "image", "inspect", image))
	if err != nil {
		if _, err = ex.Run(ctx, shell.Join("docker", "pull", image)); err != nil {
			return nil, i18n.Errorf("não foi possível obter a imagem para sugerir a porta: %w", err)
		}
		output, err = ex.Run(ctx, shell.Join("docker", "image", "inspect", image))
		if err != nil {
			return nil, err
		}
	}
	ports := diagnose.ExposedPorts([]byte(output))
	if ports == nil {
		ports = []int{}
	}
	return ports, nil
}
