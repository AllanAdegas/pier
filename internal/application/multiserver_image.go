package application

import (
	"context"
	"strings"
	"time"

	"pier/internal/docker"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
	"pier/internal/registry"
)

// produce makes the image once, on the build server, and pins it by digest
// whenever it has to reach another server.
func (r *rollout) produce(ctx context.Context) error {
	j, err := r.job(ctx, r.plan.buildServer)
	if err != nil {
		return err
	}
	if r.plan.buildServer != r.app.ServerID {
		r.log.Info("Construção da imagem no servidor %s.", j.server.Name)
	}
	if err := docker.EnsureNetwork(ctx, j.ex); err != nil {
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()
		j.cleanupBuildDir(cleanupCtx)
	}()
	image, err := r.buildImage(ctx, j)
	if err != nil {
		return err
	}
	// pier.yaml and the commit fetched on the build server apply everywhere.
	r.app.Config = j.app.Config
	r.deployment.CommitSHA, r.deployment.CommitMessage = j.deployment.CommitSHA, j.deployment.CommitMessage
	r.image = image
	return nil
}

func (r *rollout) buildImage(ctx context.Context, j *job) (string, error) {
	if pinned := j.deployment.RollbackImage; registry.Immutable(pinned) {
		// Rollback or promotion of an image already in a registry: the same
		// digest is released again, with no build and no new push.
		if err := r.d.ensureImage(ctx, j, pinned); err != nil {
			return "", err
		}
		return pinned, j.checkImageLabels(ctx, pinned)
	}
	local, err := r.d.produceImage(ctx, j)
	if err != nil {
		return "", err
	}
	if err := j.checkImageLabels(ctx, local); err != nil {
		return "", err
	}
	if !r.plan.distributed() {
		return local, nil
	}
	return r.pin(ctx, j, local)
}

// pin turns a local image into a digest reference every server can pull: the
// registry digest it was pulled with, or a new push to the placement registry.
func (r *rollout) pin(ctx context.Context, j *job, local string) (string, error) {
	digest, err := r.d.images.Digest(ctx, j.ex, local)
	if err == nil {
		j.log.Info("Imagem fixada pelo digest %s.", digest)
		return digest, nil
	}
	if !registry.IsNoDigest(err) {
		return "", err
	}
	if r.plan.RegistryID == "" || r.plan.Repository == "" {
		return "", i18n.Errorf("configure um registro de imagens e um repositório na aba Destinos: a imagem precisa chegar aos outros servidores")
	}
	j.log.Info("Enviando a imagem para o registro de imagens.")
	digest, err = r.d.images.Publish(ctx, r.plan.RegistryID, j.ex, local, r.plan.Repository, "pier-"+j.deployment.ID, j.log.Output)
	if err != nil {
		return "", err
	}
	j.log.Info("Imagem publicada: %s.", digest)
	return digest, nil
}

// distribute pulls the pinned image on every server before any of them
// changes, and refuses a platform the server cannot run.
func (r *rollout) distribute(ctx context.Context) error {
	return r.distributeTo(ctx, r.plan.servers)
}

// distributeTo pulls the pinned image on the given servers.
func (r *rollout) distributeTo(ctx context.Context, servers []string) error {
	if !registry.Immutable(r.image) {
		return nil
	}
	for _, id := range servers {
		j, err := r.job(ctx, id)
		if err == nil {
			err = r.d.ensureImage(ctx, j, r.image)
		}
		if err == nil {
			err = checkPlatform(ctx, j, r.image)
		}
		if err != nil {
			r.record(ctx, id, PlacementFailed, "", "", err)
			return err
		}
	}
	return nil
}

// ensureImage pulls a digest reference the server does not have yet, with
// the registry credential of its host.
func (d *Deployer) ensureImage(ctx context.Context, j *job, image string) error {
	if d.images == nil || !registry.Immutable(image) || docker.ImageExists(ctx, j.ex, image) {
		return nil
	}
	j.log.Info("Baixando %s em %s.", image, j.server.Name)
	if err := d.images.Pull(ctx, j.ex, image, j.log.Output); err != nil {
		return i18n.Errorf("baixar a imagem em %s: %w", j.server.Name, err)
	}
	return nil
}

// ensureRegistryImage is the produceImage hook: a rollback or promotion of a
// digest reference pulls it first when it is not on the server. Any other
// deployment is unchanged.
func (d *Deployer) ensureRegistryImage(ctx context.Context, j *job) error {
	if j.deployment.Trigger != TriggerRollback && j.deployment.Trigger != TriggerPromotion {
		return nil
	}
	return d.ensureImage(ctx, j, j.deployment.RollbackImage)
}

// checkPlatform compares the platform of the image with the server's.
func checkPlatform(ctx context.Context, j *job, image string) error {
	checkCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	serverPlatform, err := j.ex.Run(checkCtx, "docker version --format '{{.Server.Os}}/{{.Server.Arch}}'")
	if err != nil {
		return nil
	}
	imagePlatform, err := j.ex.Run(checkCtx, shell.Join("docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image))
	serverPlatform, imagePlatform = strings.TrimSpace(serverPlatform), strings.TrimSpace(imagePlatform)
	if err != nil || serverPlatform == "" || imagePlatform == "" || serverPlatform == imagePlatform {
		return nil
	}
	return i18n.Errorf("a imagem é %s e o servidor %s é %s: use um servidor de build da mesma arquitetura ou uma imagem multiplataforma",
		imagePlatform, j.server.Name, serverPlatform)
}

// pullImage is the image-pack hook: it pulls with the credential stored for
// the image's registry and, without one, runs the same plain pull as before.
func (j *job) pullImage(ctx context.Context, image string) error {
	if j.registries == nil {
		return j.ex.Stream(ctx, shell.Join("docker", "pull", image), j.log.Output)
	}
	return j.registries.Pull(ctx, j.ex, image, j.log.Output)
}
