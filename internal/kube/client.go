package kube

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	requestTimeout = 30 * time.Second
	// fieldManager identifies Pier as the owner of the fields it applies.
	fieldManager  = "pier"
	maxErrorBytes = 4096
)

// Client talks to the API server of one cluster.
type Client struct {
	config Config
	// http serves ordinary requests; stream has no overall timeout and is
	// used for following pod logs.
	http   *http.Client
	stream *http.Client
}

// StatusError is an error answered by the API server.
type StatusError struct {
	Code    int
	Reason  string
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("o cluster respondeu HTTP %d", e.Code)
}

// IsNotFound reports whether the error is a 404 from the API server.
func IsNotFound(err error) bool {
	var status *StatusError
	return errors.As(err, &status) && status.Code == http.StatusNotFound
}

// NewClient builds a client from the connection settings.
func NewClient(config Config) (*Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: config.Insecure} //nolint:gosec // opt-in copied from the user's kubeconfig
	if len(config.CAData) > 0 {
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(config.CAData) {
			return nil, errors.New("o certificado da CA no kubeconfig não é um PEM válido")
		}
		tlsConfig.RootCAs = pool
	}
	if len(config.ClientCert) > 0 {
		pair, err := tls.X509KeyPair(config.ClientCert, config.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("certificado de cliente inválido no kubeconfig: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{pair}
	}
	transport := &http.Transport{
		TLSClientConfig:       tlsConfig,
		ResponseHeaderTimeout: requestTimeout,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
	}
	return &Client{
		config: config,
		http:   &http.Client{Transport: transport, Timeout: requestTimeout},
		stream: &http.Client{Transport: transport},
	}, nil
}

// Server is the API address this client talks to.
func (c *Client) Server() string { return c.config.Server }

func (c *Client) newRequest(ctx context.Context, method, path string, query url.Values, body []byte, contentType string) (*http.Request, error) {
	target := c.config.Server + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "pier")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	switch {
	case c.config.Token != "":
		request.Header.Set("Authorization", "Bearer "+c.config.Token)
	case c.config.Username != "":
		request.SetBasicAuth(c.config.Username, c.config.Password)
	}
	return request, nil
}

// do performs a request and decodes a JSON answer into out (when not nil).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body []byte, contentType string, out any) error {
	request, err := c.newRequest(ctx, method, path, query, body, contentType)
	if err != nil {
		return err
	}
	response, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("não foi possível contatar o cluster: %w", unwrapURLError(err))
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return readStatusError(response)
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		return nil
	}
	if err := json.NewDecoder(response.Body).Decode(out); err != nil {
		return fmt.Errorf("resposta inesperada do cluster: %w", err)
	}
	return nil
}

func readStatusError(response *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(response.Body, maxErrorBytes))
	status := &StatusError{Code: response.StatusCode}
	var payload struct {
		Message string `json:"message"`
		Reason  string `json:"reason"`
	}
	if json.Unmarshal(raw, &payload) == nil && payload.Message != "" {
		status.Message, status.Reason = payload.Message, payload.Reason
	} else if text := strings.TrimSpace(string(raw)); text != "" {
		status.Message = text
	}
	return status
}

// unwrapURLError drops the URL from transport errors: it is noise for the user.
func unwrapURLError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return urlErr.Err
	}
	return err
}

// Version returns the cluster version, e.g. "v1.31.4+k3s1".
func (c *Client) Version(ctx context.Context) (string, error) {
	var payload struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := c.do(ctx, http.MethodGet, "/version", nil, nil, "", &payload); err != nil {
		return "", err
	}
	return payload.GitVersion, nil
}

// Object is any Kubernetes resource, kept as generic JSON.
type Object = map[string]any

// List returns the objects of a resource. An empty namespace means every
// namespace (or the cluster scope, for cluster-wide resources).
func (c *Client) List(ctx context.Context, resource Resource, namespace string) ([]Object, error) {
	var payload struct {
		Items []Object `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, resource.Path(namespace, ""), nil, nil, "", &payload); err != nil {
		return nil, err
	}
	// Items of a list omit apiVersion and kind; restore them so each object is complete.
	for _, item := range payload.Items {
		item["apiVersion"], item["kind"] = resource.APIVersion(), resource.Kind
	}
	return payload.Items, nil
}

// Get returns one object.
func (c *Client) Get(ctx context.Context, resource Resource, namespace, name string) (Object, error) {
	var object Object
	err := c.do(ctx, http.MethodGet, resource.Path(namespace, name), nil, nil, "", &object)
	return object, err
}

// Delete removes one object; dependents are removed in the background.
func (c *Client) Delete(ctx context.Context, resource Resource, namespace, name string) error {
	query := url.Values{"propagationPolicy": {"Background"}}
	return c.do(ctx, http.MethodDelete, resource.Path(namespace, name), query, nil, "", nil)
}

// ApplyObject creates or updates one object with server-side apply. Pier
// takes ownership of the fields it sends (force), like `kubectl apply
// --server-side --force-conflicts`.
func (c *Client) ApplyObject(ctx context.Context, resource Resource, namespace string, object Object) error {
	metadata, _ := object["metadata"].(map[string]any)
	name, _ := metadata["name"].(string)
	if name == "" {
		return errors.New("o manifesto não tem metadata.name")
	}
	body, err := json.Marshal(object)
	if err != nil {
		return err
	}
	query := url.Values{"fieldManager": {fieldManager}, "force": {"true"}}
	return c.do(ctx, http.MethodPatch, resource.Path(namespace, name), query, body, "application/apply-patch+yaml", nil)
}

// MergePatch applies a JSON merge patch to an object or to one of its
// subresources (e.g. "scale").
func (c *Client) MergePatch(ctx context.Context, resource Resource, namespace, name, subresource string, patch any) error {
	body, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	path := resource.Path(namespace, name)
	if subresource != "" {
		path += "/" + subresource
	}
	return c.do(ctx, http.MethodPatch, path, nil, body, "application/merge-patch+json", nil)
}

// Scale sets the number of replicas of a workload.
func (c *Client) Scale(ctx context.Context, resource Resource, namespace, name string, replicas int) error {
	patch := map[string]any{"spec": map[string]any{"replicas": replicas}}
	return c.MergePatch(ctx, resource, namespace, name, "scale", patch)
}

// Restart triggers a rolling restart by touching the pod template, exactly
// as `kubectl rollout restart` does.
func (c *Client) Restart(ctx context.Context, resource Resource, namespace, name string) error {
	patch := map[string]any{"spec": map[string]any{"template": map[string]any{"metadata": map[string]any{
		"annotations": map[string]any{"pier.io/restartedAt": time.Now().UTC().Format(time.RFC3339)},
	}}}}
	return c.MergePatch(ctx, resource, namespace, name, "", patch)
}

// PodLogs opens the log of a pod container; the caller closes the stream.
func (c *Client) PodLogs(ctx context.Context, namespace, pod, container string, tail int, follow bool) (io.ReadCloser, error) {
	query := url.Values{"tailLines": {strconv.Itoa(tail)}, "timestamps": {"true"}}
	if follow {
		query.Set("follow", "true")
	}
	if container != "" {
		query.Set("container", container)
	}
	path := "/api/v1/namespaces/" + url.PathEscape(namespace) + "/pods/" + url.PathEscape(pod) + "/log"
	request, err := c.newRequest(ctx, http.MethodGet, path, query, nil, "")
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "*/*")
	response, err := c.stream.Do(request)
	if err != nil {
		return nil, fmt.Errorf("não foi possível contatar o cluster: %w", unwrapURLError(err))
	}
	if response.StatusCode >= 300 {
		defer response.Body.Close()
		return nil, readStatusError(response)
	}
	return response.Body, nil
}
