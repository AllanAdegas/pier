package application

import (
	"context"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

// Promote queues a deployment of the target application that releases the
// image the source application runs now, pinned by its ID, with the target's
// own variables and release command. Nothing is rebuilt.
func (s *Service) Promote(ctx context.Context, sourceID, targetID string) (Deployment, error) {
	if strings.TrimSpace(targetID) == "" {
		return Deployment{}, apperr.Invalid("Informe a aplicação de destino.")
	}
	if sourceID == targetID {
		return Deployment{}, apperr.Invalid("Escolha uma aplicação de destino diferente da origem.")
	}
	source, err := s.Get(ctx, sourceID)
	if err != nil {
		return Deployment{}, err
	}
	target, err := s.Get(ctx, targetID)
	if err != nil {
		return Deployment{}, err
	}
	if err := s.checkDeploy(ctx, target); err != nil {
		return Deployment{}, err
	}
	if source.CurrentImage == "" {
		return Deployment{}, apperr.Conflict("A aplicação de origem ainda não tem uma versão implantada para promover.")
	}
	if deployment, handled, err := s.promoteByDigest(ctx, source, target); handled {
		return deployment, err
	}
	if source.ServerID != target.ServerID {
		return Deployment{}, apperr.Conflict("As aplicações estão em servidores diferentes. Promover entre servidores exige um " +
			"registry de imagens, que ainda não está disponível: use um destino no mesmo servidor ou faça o deploy pelo código.")
	}
	imageID, err := s.imageID(ctx, source)
	if err != nil {
		return Deployment{}, err
	}
	deployment := Deployment{
		ApplicationID: targetID, Trigger: TriggerPromotion, RollbackImage: imageID, PromotedFrom: sourceID,
		Source: i18n.Instance().Sprintf("Promovido de %s (%s)", source.Name, shortImageID(imageID)),
	}
	if origin, err := s.store.deploymentOfImage(ctx, sourceID, source.CurrentImage); err == nil {
		deployment.CommitSHA, deployment.CommitMessage = origin.CommitSHA, origin.CommitMessage
		if origin.PromotedFrom != "" {
			// A promoted image keeps the identity of the application that built it.
			deployment.PromotedFrom = origin.PromotedFrom
		}
	}
	return s.enqueue(ctx, deployment)
}

// imageID resolves the current image of an application to its immutable ID,
// so a later deployment of the source cannot change what is promoted.
func (s *Service) imageID(ctx context.Context, app Application) (string, error) {
	ex, _, err := s.connector.Executor(ctx, app.ServerID)
	if err != nil {
		return "", apperr.Unavailable("Não foi possível consultar o servidor: %s", err)
	}
	output, err := ex.Run(ctx, shell.Join("docker", "image", "inspect", "--format", "{{.Id}}", app.CurrentImage))
	id := strings.TrimSpace(output)
	if err != nil || !strings.HasPrefix(id, "sha256:") {
		return "", apperr.Conflict("A imagem atual da aplicação de origem não está mais no servidor. Faça um novo deploy da origem antes de promover.")
	}
	return id, nil
}

func shortImageID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// promotedImage tags the promoted image under the target's own namespace, so
// pruning and rollback of the target never touch the source's tags.
func (j *job) promotedImage(ctx context.Context) (string, error) {
	imageID := j.deployment.RollbackImage
	j.log.Info("Promovendo a imagem %s sem novo build.", shortImageID(imageID))
	if !docker.ImageExists(ctx, j.ex, imageID) {
		return "", i18n.Errorf("a imagem promovida %s não está mais disponível no servidor", shortImageID(imageID))
	}
	tag := j.imageTag()
	if _, err := j.ex.Run(ctx, shell.Join("docker", "tag", imageID, tag)); err != nil {
		return "", i18n.Errorf("não foi possível marcar a imagem promovida: %w", err)
	}
	return tag, nil
}
