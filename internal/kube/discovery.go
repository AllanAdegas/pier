package kube

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

// Resource identifies one kind of object the cluster serves, as discovered
// from the API server (so custom resources work like built-in ones).
type Resource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Name       string   `json:"name"` // plural, e.g. "deployments"
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs"`
}

// APIVersion renders "group/version", or just "version" for the core group.
func (r Resource) APIVersion() string {
	if r.Group == "" {
		return r.Version
	}
	return r.Group + "/" + r.Version
}

// Path builds the REST path of the collection or of one object.
func (r Resource) Path(namespace, name string) string {
	path := "/apis/" + r.Group + "/" + r.Version
	if r.Group == "" {
		path = "/api/" + r.Version
	}
	if r.Namespaced && namespace != "" {
		path += "/namespaces/" + url.PathEscape(namespace)
	}
	path += "/" + r.Name
	if name != "" {
		path += "/" + url.PathEscape(name)
	}
	return path
}

// Can reports whether the resource supports a verb (list, delete, patch…).
func (r Resource) Can(verb string) bool { return slices.Contains(r.Verbs, verb) }

type resourceList struct {
	Resources []struct {
		Name       string   `json:"name"`
		Kind       string   `json:"kind"`
		Namespaced bool     `json:"namespaced"`
		Verbs      []string `json:"verbs"`
	} `json:"resources"`
}

type groupList struct {
	Groups []struct {
		Name             string `json:"name"`
		PreferredVersion struct {
			Version string `json:"version"`
		} `json:"preferredVersion"`
	} `json:"groups"`
}

// Resources discovers every listable resource of the cluster, using the
// preferred version of each API group.
func (c *Client) Resources(ctx context.Context) ([]Resource, error) {
	var groups groupList
	if err := c.do(ctx, http.MethodGet, "/apis", nil, nil, "", &groups); err != nil {
		return nil, err
	}
	type target struct{ group, version string }
	targets := []target{{"", "v1"}}
	for _, group := range groups.Groups {
		targets = append(targets, target{group.Name, group.PreferredVersion.Version})
	}

	var (
		mu        sync.Mutex
		wait      sync.WaitGroup
		resources []Resource
		limiter   = make(chan struct{}, 8)
	)
	for _, item := range targets {
		wait.Add(1)
		go func() {
			defer wait.Done()
			limiter <- struct{}{}
			defer func() { <-limiter }()
			found, err := c.groupResources(ctx, item.group, item.version)
			if err != nil {
				// One broken aggregated API must not hide the rest of the cluster.
				return
			}
			mu.Lock()
			resources = append(resources, found...)
			mu.Unlock()
		}()
	}
	wait.Wait()
	if len(resources) == 0 {
		return nil, errors.New("o cluster não informou nenhum recurso")
	}
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Group != resources[j].Group {
			return resources[i].Group < resources[j].Group
		}
		return resources[i].Name < resources[j].Name
	})
	return resources, nil
}

func (c *Client) groupResources(ctx context.Context, group, version string) ([]Resource, error) {
	path := "/apis/" + group + "/" + version
	if group == "" {
		path = "/api/" + version
	}
	var list resourceList
	if err := c.do(ctx, http.MethodGet, path, nil, nil, "", &list); err != nil {
		return nil, err
	}
	resources := []Resource{}
	for _, item := range list.Resources {
		// Names with "/" are subresources (pods/log, deployments/scale).
		if strings.Contains(item.Name, "/") || !slices.Contains(item.Verbs, "list") {
			continue
		}
		resources = append(resources, Resource{
			Group: group, Version: version, Name: item.Name, Kind: item.Kind,
			Namespaced: item.Namespaced, Verbs: item.Verbs,
		})
	}
	return resources, nil
}

// FindResource locates a discovered resource by group and plural name.
func FindResource(resources []Resource, group, name string) (Resource, bool) {
	for _, resource := range resources {
		if resource.Group == group && resource.Name == name {
			return resource, true
		}
	}
	return Resource{}, false
}

// findByKind locates the resource that serves an apiVersion/kind pair.
func findByKind(resources []Resource, apiVersion, kind string) (Resource, bool) {
	group, _, hasGroup := strings.Cut(apiVersion, "/")
	if !hasGroup {
		group = ""
	}
	for _, resource := range resources {
		if resource.Group == group && resource.Kind == kind {
			return resource, true
		}
	}
	return Resource{}, false
}

// Applied reports what happened to one object of a manifest.
type Applied struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// Apply creates or updates every object of a (possibly multi-document) YAML
// manifest. Namespaced objects without a namespace go to defaultNamespace.
// It stops at the first failure and reports what was applied until then.
func (c *Client) Apply(ctx context.Context, resources []Resource, manifest []byte, defaultNamespace string) ([]Applied, error) {
	objects, err := DecodeManifest(manifest)
	if err != nil {
		return nil, err
	}
	if len(objects) == 0 {
		return nil, errors.New("o manifesto está vazio")
	}
	applied := []Applied{}
	for _, object := range objects {
		apiVersion, _ := object["apiVersion"].(string)
		kind, _ := object["kind"].(string)
		resource, found := findByKind(resources, apiVersion, kind)
		if !found {
			return applied, fmt.Errorf("o cluster não conhece %s %s", apiVersion, kind)
		}
		metadata, _ := object["metadata"].(map[string]any)
		if metadata == nil {
			return applied, fmt.Errorf("%s sem metadata", kind)
		}
		namespace, _ := metadata["namespace"].(string)
		if resource.Namespaced && namespace == "" {
			namespace = defaultNamespace
			metadata["namespace"] = namespace
		}
		if !resource.Namespaced {
			namespace = ""
		}
		name, _ := metadata["name"].(string)
		if err := c.ApplyObject(ctx, resource, namespace, object); err != nil {
			return applied, fmt.Errorf("%s %s: %w", kind, name, err)
		}
		applied = append(applied, Applied{Kind: kind, Name: name, Namespace: namespace})
	}
	return applied, nil
}

// DecodeManifest splits a YAML stream into objects, skipping empty documents.
func DecodeManifest(manifest []byte) ([]Object, error) {
	decoder := yaml.NewDecoder(bytes.NewReader(manifest))
	objects := []Object{}
	for {
		var object map[string]any
		err := decoder.Decode(&object)
		if errors.Is(err, io.EOF) {
			return objects, nil
		}
		if err != nil {
			return nil, fmt.Errorf("o manifesto não é um YAML válido: %w", err)
		}
		if len(object) == 0 {
			continue
		}
		if object["kind"] == nil || object["apiVersion"] == nil {
			return nil, errors.New("todo documento do manifesto precisa de apiVersion e kind")
		}
		objects = append(objects, object)
	}
}

// EncodeYAML renders an object as YAML for display, dropping the
// server-managed bookkeeping that only adds noise.
func EncodeYAML(object Object) (string, error) {
	if metadata, ok := object["metadata"].(map[string]any); ok {
		delete(metadata, "managedFields")
	}
	rendered, err := yaml.Marshal(object)
	return string(rendered), err
}
