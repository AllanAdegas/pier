package cluster

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/kube"
	"pier/internal/platform/apperr"
)

// WorkloadSpec is the short form for the most common thing to run on a
// cluster: an image, some replicas and, optionally, a public host. Anything
// beyond it is done by applying YAML.
type WorkloadSpec struct {
	Namespace string            `json:"namespace"`
	Name      string            `json:"name"`
	Image     string            `json:"image"`
	Replicas  int               `json:"replicas"`
	Port      int               `json:"port"`
	Env       map[string]string `json:"env"`
	// Host publishes the workload through an Ingress when set.
	Host string `json:"host"`
}

// dnsLabel is the format Kubernetes requires for names used in DNS.
var dnsLabel = regexp.MustCompile(`^[a-z]([-a-z0-9]{0,61}[a-z0-9])?$`)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (w *WorkloadSpec) validate() error {
	fields := apperr.Fields{}
	w.Name = strings.TrimSpace(w.Name)
	w.Image = strings.TrimSpace(w.Image)
	w.Namespace = orDefault(strings.TrimSpace(w.Namespace), "default")
	w.Host = strings.ToLower(strings.TrimSpace(w.Host))
	if w.Replicas == 0 {
		w.Replicas = 1
	}
	if !dnsLabel.MatchString(w.Name) {
		fields.Add("name", "Use letras minúsculas, números e hífens, começando por letra (até 63 caracteres).")
	}
	if !dnsLabel.MatchString(w.Namespace) {
		fields.Add("namespace", "Namespace inválido.")
	}
	if !docker.ValidImage(w.Image) {
		fields.Add("image", "Informe uma imagem válida, ex.: nginx:1.27.")
	}
	if w.Replicas < 1 || w.Replicas > 100 {
		fields.Add("replicas", "Use entre 1 e 100 réplicas.")
	}
	if w.Port < 0 || w.Port > 65535 {
		fields.Add("port", "Porta fora do intervalo 1–65535.")
	}
	if w.Host != "" {
		if w.Port == 0 {
			fields.Add("port", "Informe a porta do container para publicar um domínio.")
		}
		if route, err := compose.ParseRoute("http://" + w.Host); err != nil || route.Path != "" {
			fields.Add("host", "Informe apenas o hostname, ex.: app.exemplo.com.")
		}
	}
	for key := range w.Env {
		if !envKey.MatchString(key) {
			fields.Add("env", "Nome de variável inválido: %q.", key)
		}
	}
	return fields.Err()
}

// manifest renders the objects of a workload: a Deployment, plus a Service
// when it listens on a port and an Ingress when it has a host.
func (w WorkloadSpec) manifest() ([]byte, error) {
	labels := map[string]any{"app.kubernetes.io/name": w.Name, "app.kubernetes.io/managed-by": "pier"}
	container := map[string]any{"name": w.Name, "image": w.Image}
	if len(w.Env) > 0 {
		container["env"] = envList(w.Env)
	}
	if w.Port > 0 {
		container["ports"] = []any{map[string]any{"containerPort": w.Port}}
	}
	meta := func() map[string]any {
		return map[string]any{"name": w.Name, "namespace": w.Namespace, "labels": labels}
	}
	objects := []map[string]any{}
	if w.Namespace != "default" {
		objects = append(objects, map[string]any{
			"apiVersion": "v1", "kind": "Namespace", "metadata": map[string]any{"name": w.Namespace},
		})
	}
	objects = append(objects, map[string]any{
		"apiVersion": "apps/v1", "kind": "Deployment", "metadata": meta(),
		"spec": map[string]any{
			"replicas": w.Replicas,
			"selector": map[string]any{"matchLabels": map[string]any{"app.kubernetes.io/name": w.Name}},
			"template": map[string]any{
				"metadata": map[string]any{"labels": labels},
				"spec":     map[string]any{"containers": []any{container}},
			},
		},
	})
	if w.Port > 0 {
		objects = append(objects, map[string]any{
			"apiVersion": "v1", "kind": "Service", "metadata": meta(),
			"spec": map[string]any{
				"selector": map[string]any{"app.kubernetes.io/name": w.Name},
				"ports":    []any{map[string]any{"port": w.Port, "targetPort": w.Port}},
			},
		})
	}
	if w.Host != "" {
		objects = append(objects, map[string]any{
			"apiVersion": "networking.k8s.io/v1", "kind": "Ingress", "metadata": meta(),
			"spec": map[string]any{"rules": []any{map[string]any{
				"host": w.Host,
				"http": map[string]any{"paths": []any{map[string]any{
					"path": "/", "pathType": "Prefix",
					"backend": map[string]any{"service": map[string]any{
						"name": w.Name, "port": map[string]any{"number": w.Port},
					}},
				}}},
			}}},
		})
	}
	var rendered strings.Builder
	for index, object := range objects {
		if index > 0 {
			rendered.WriteString("---\n")
		}
		document, err := yaml.Marshal(object)
		if err != nil {
			return nil, err
		}
		rendered.Write(document)
	}
	return []byte(rendered.String()), nil
}

func envList(env map[string]string) []any {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	// Sorted so the same input always renders the same manifest.
	sort.Strings(keys)
	entries := make([]any, len(keys))
	for i, key := range keys {
		entries[i] = map[string]any{"name": key, "value": env[key]}
	}
	return entries
}

// CreateWorkload validates the short form and applies its manifest.
func (s *Service) CreateWorkload(ctx context.Context, id string, spec WorkloadSpec) ([]kube.Applied, string, error) {
	if err := spec.validate(); err != nil {
		return nil, "", err
	}
	manifest, err := spec.manifest()
	if err != nil {
		return nil, "", err
	}
	applied, err := s.Apply(ctx, id, string(manifest), spec.Namespace)
	return applied, string(manifest), err
}
