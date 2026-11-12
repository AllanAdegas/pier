package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"pier/internal/kube"
	"pier/internal/platform/apperr"
	"pier/internal/platform/i18n"
)

// ResourceRef addresses a kind of resource the way the API server names it.
type ResourceRef struct {
	Group    string `json:"group"`
	Resource string `json:"resource"`
}

// ObjectRef addresses one object.
type ObjectRef struct {
	ResourceRef
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// Row is the summary of one object shown in a listing. It never carries the
// object's contents (so listing Secrets exposes no secret).
type Row struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Created   string `json:"created"`
	// Status is a short human reading ("3/3 prontos", "Running").
	Status string `json:"status"`
	// Health classifies the status: "ok", "progressing", "error" or "".
	Health string `json:"health"`
	// Info is secondary detail (image, hosts, type…).
	Info string `json:"info"`

	language i18n.Language
}

// Resources lists the kinds of resource the cluster serves. The discovery is
// cached for a few minutes: it costs one request per API group.
func (s *Service) Resources(ctx context.Context, id string) ([]kube.Resource, error) {
	current, err := s.session(ctx, id)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	fresh := current.resources != nil && time.Since(current.discoveredAt) < discoveryLifetime
	cached := current.resources
	s.mu.Unlock()
	if fresh {
		return cached, nil
	}
	discovered, err := current.client.Resources(ctx)
	if err != nil {
		return nil, unreachable(err)
	}
	s.mu.Lock()
	current.resources, current.discoveredAt = discovered, time.Now()
	s.mu.Unlock()
	return discovered, nil
}

func (s *Service) resolve(ctx context.Context, id string, ref ResourceRef) (*kube.Client, kube.Resource, error) {
	resources, err := s.Resources(ctx, id)
	if err != nil {
		return nil, kube.Resource{}, err
	}
	resource, found := kube.FindResource(resources, ref.Group, ref.Resource)
	if !found {
		return nil, kube.Resource{}, apperr.NotFound("Tipo de recurso")
	}
	client, err := s.client(ctx, id)
	return client, resource, err
}

// Rows lists the objects of a resource as summaries, newest first.
func (s *Service) Rows(ctx context.Context, id string, ref ResourceRef, namespace string) ([]Row, error) {
	client, resource, err := s.resolve(ctx, id, ref)
	if err != nil {
		return nil, err
	}
	objects, err := client.List(ctx, resource, namespace)
	if err != nil {
		return nil, unreachable(err)
	}
	rows := make([]Row, len(objects))
	for i, object := range objects {
		rows[i] = summarize(resource.Kind, object, i18n.FromContext(ctx))
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Created > rows[j].Created })
	return rows, nil
}

// Manifest returns one object as YAML.
func (s *Service) Manifest(ctx context.Context, id string, ref ObjectRef) (string, error) {
	client, resource, err := s.resolve(ctx, id, ref.ResourceRef)
	if err != nil {
		return "", err
	}
	object, err := client.Get(ctx, resource, ref.Namespace, ref.Name)
	if err != nil {
		return "", unreachable(err)
	}
	return kube.EncodeYAML(object)
}

// Apply creates or updates the objects of a YAML manifest.
func (s *Service) Apply(ctx context.Context, id, manifest, defaultNamespace string) ([]kube.Applied, error) {
	if strings.TrimSpace(manifest) == "" {
		return nil, apperr.Invalid("Informe o manifesto YAML.")
	}
	resources, err := s.Resources(ctx, id)
	if err != nil {
		return nil, err
	}
	client, err := s.client(ctx, id)
	if err != nil {
		return nil, err
	}
	applied, err := client.Apply(ctx, resources, []byte(manifest), orDefault(strings.TrimSpace(defaultNamespace), "default"))
	if err != nil {
		prefix := ""
		if len(applied) > 0 {
			prefix = fmt.Sprintf("%d objeto(s) aplicados antes da falha. ", len(applied))
		}
		return applied, apperr.Invalid("%s%s", prefix, capitalize(err.Error()))
	}
	// A manifest may have installed new kinds (CRDs): discover again next time.
	s.mu.Lock()
	if current, ok := s.clients[id]; ok {
		current.resources = nil
	}
	s.mu.Unlock()
	return applied, nil
}

// DeleteObject removes one object from the cluster.
func (s *Service) DeleteObject(ctx context.Context, id string, ref ObjectRef) error {
	client, resource, err := s.resolve(ctx, id, ref.ResourceRef)
	if err != nil {
		return err
	}
	return unreachable(client.Delete(ctx, resource, ref.Namespace, ref.Name))
}

// scalable lists the workload kinds that have a replica count.
var scalable = map[string]bool{"Deployment": true, "StatefulSet": true, "ReplicaSet": true}

// restartable lists the workload kinds with a pod template to roll.
var restartable = map[string]bool{"Deployment": true, "StatefulSet": true, "DaemonSet": true}

// Scale sets the replica count of a workload.
func (s *Service) Scale(ctx context.Context, id string, ref ObjectRef, replicas int) error {
	if replicas < 0 || replicas > 1000 {
		return apperr.Invalid("Informe um número de réplicas entre 0 e 1000.")
	}
	client, resource, err := s.resolve(ctx, id, ref.ResourceRef)
	if err != nil {
		return err
	}
	if !scalable[resource.Kind] {
		return apperr.Conflict("%s não tem número de réplicas.", resource.Kind)
	}
	return unreachable(client.Scale(ctx, resource, ref.Namespace, ref.Name, replicas))
}

// Restart rolls the pods of a workload.
func (s *Service) Restart(ctx context.Context, id string, ref ObjectRef) error {
	client, resource, err := s.resolve(ctx, id, ref.ResourceRef)
	if err != nil {
		return err
	}
	if !restartable[resource.Kind] {
		return apperr.Conflict("%s não pode ser reiniciado.", resource.Kind)
	}
	return unreachable(client.Restart(ctx, resource, ref.Namespace, ref.Name))
}

// PodLogs opens the log stream of a pod; the caller closes it.
func (s *Service) PodLogs(ctx context.Context, id, namespace, pod, container string, tail int, follow bool) (io.ReadCloser, error) {
	client, err := s.client(ctx, id)
	if err != nil {
		return nil, err
	}
	stream, err := client.PodLogs(ctx, namespace, pod, container, tail, follow)
	return stream, unreachable(err)
}

// Overview is the landing summary of a cluster.
type Overview struct {
	Nodes       []Row `json:"nodes"`
	Namespaces  int   `json:"namespaces"`
	Pods        int   `json:"pods"`
	PodsRunning int   `json:"pods_running"`
	Deployments int   `json:"deployments"`
	// Events holds the most recent warnings.
	Events []Row `json:"events"`
}

const overviewEvents = 15

// Overview collects nodes, counts and recent warnings.
func (s *Service) Overview(ctx context.Context, id string) (Overview, error) {
	var overview Overview
	var err error
	if overview.Nodes, err = s.Rows(ctx, id, ResourceRef{Resource: "nodes"}, ""); err != nil {
		return Overview{}, err
	}
	namespaces, err := s.Rows(ctx, id, ResourceRef{Resource: "namespaces"}, "")
	if err != nil {
		return Overview{}, err
	}
	pods, err := s.Rows(ctx, id, ResourceRef{Resource: "pods"}, "")
	if err != nil {
		return Overview{}, err
	}
	deployments, err := s.Rows(ctx, id, ResourceRef{Group: "apps", Resource: "deployments"}, "")
	if err != nil {
		return Overview{}, err
	}
	events, err := s.Rows(ctx, id, ResourceRef{Resource: "events"}, "")
	if err != nil {
		return Overview{}, err
	}
	overview.Namespaces, overview.Pods, overview.Deployments = len(namespaces), len(pods), len(deployments)
	for _, pod := range pods {
		if pod.Health == "ok" {
			overview.PodsRunning++
		}
	}
	overview.Events = []Row{}
	for _, event := range events {
		if event.Health == "error" && len(overview.Events) < overviewEvents {
			overview.Events = append(overview.Events, event)
		}
	}
	return overview, nil
}

// unreachable turns client failures into errors the API layer understands.
// What the API server rejected (4xx) is the caller's to fix; anything else
// means the cluster could not be used.
func unreachable(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	var status *kube.StatusError
	if errors.As(err, &status) {
		switch status.Code {
		case http.StatusNotFound:
			return apperr.NotFound("Objeto")
		case http.StatusBadRequest, http.StatusUnprocessableEntity:
			return apperr.Invalid("O cluster rejeitou o pedido: %s", status.Message)
		case http.StatusConflict:
			return apperr.Conflict("Conflito no cluster: %s", status.Message)
		case http.StatusUnauthorized, http.StatusForbidden:
			return apperr.Conflict("A credencial do kubeconfig não tem permissão para isso: %s", status.Message)
		}
	}
	return apperr.Unavailable("O cluster recusou a operação: %s", err)
}
