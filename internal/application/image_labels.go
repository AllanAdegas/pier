package application

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"pier/internal/docker"
	"pier/internal/platform/i18n"
	"pier/internal/platform/shell"
)

func (j *job) checkImageLabels(ctx context.Context, image string) error {
	output, err := j.ex.Run(ctx, shell.Join("docker", "image", "inspect", "--format", "{{json .Config.Labels}}", image))
	if err != nil {
		return i18n.Errorf("não foi possível verificar as etiquetas da imagem: %w", err)
	}
	var labels map[string]string
	if err := json.Unmarshal([]byte(output), &labels); err != nil {
		return i18n.Errorf("a imagem retornou etiquetas inválidas; o deploy foi interrompido")
	}
	// A previously successful Pier build stays trusted on rollback even if
	// the editable build pack has since changed to image. Only the two labels
	// Pier sets during build are allowed; all routing labels remain forbidden.
	// A promoted image was built by Pier for another application: it keeps
	// that application's identity label, recorded with the deployment.
	trustedBuild := strings.HasPrefix(image, "pier/"+j.app.ID+":") && (j.app.Config.Build.Pack != PackImage ||
		j.deployment.RollbackImage == image || j.deployment.Trigger == TriggerPromotion)
	builtBy := func(id string) bool {
		return id == j.app.ID || (j.deployment.PromotedFrom != "" && id == j.deployment.PromotedFrom)
	}
	forbidden := []string{}
	for key, value := range labels {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "traefik.") || strings.HasPrefix(lower, "pier.") {
			if trustedBuild && ((key == docker.LabelManaged && value == "true") || (key == docker.LabelID && builtBy(value))) {
				continue
			}
			forbidden = append(forbidden, key)
		}
	}
	if len(forbidden) > 0 {
		sort.Strings(forbidden)
		return i18n.Errorf("a imagem contém etiquetas reservadas (%s). Remova as etiquetas traefik.* e pier.* da imagem: elas podem alterar rotas e recursos gerenciados pelo Pier", strings.Join(forbidden, ", "))
	}
	return nil
}
