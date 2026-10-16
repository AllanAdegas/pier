package kube

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is a minimal Kubernetes API server that records what it receives.
type fakeAPI struct {
	mu       sync.Mutex
	requests []recorded
}

type recorded struct {
	Method, Path, Query, ContentType, Authorization, Body string
}

func (f *fakeAPI) last() recorded {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[len(f.requests)-1]
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.requests = append(f.requests, recorded{
		Method: r.Method, Path: r.URL.Path, Query: r.URL.RawQuery, ContentType: r.Header.Get("Content-Type"),
		Authorization: r.Header.Get("Authorization"), Body: string(body),
	})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/version":
		io.WriteString(w, `{"gitVersion":"v1.31.4+k3s1"}`)
	case r.URL.Path == "/apis":
		io.WriteString(w, `{"groups":[{"name":"apps","preferredVersion":{"version":"v1"}},
			{"name":"broken.example.com","preferredVersion":{"version":"v1"}}]}`)
	case r.URL.Path == "/api/v1":
		io.WriteString(w, `{"resources":[
			{"name":"pods","kind":"Pod","namespaced":true,"verbs":["get","list","delete"]},
			{"name":"pods/log","kind":"Pod","namespaced":true,"verbs":["get"]},
			{"name":"namespaces","kind":"Namespace","namespaced":false,"verbs":["get","list","patch"]},
			{"name":"bindings","kind":"Binding","namespaced":true,"verbs":["create"]}]}`)
	case r.URL.Path == "/apis/apps/v1":
		io.WriteString(w, `{"resources":[
			{"name":"deployments","kind":"Deployment","namespaced":true,"verbs":["get","list","patch","delete"]},
			{"name":"deployments/scale","kind":"Scale","namespaced":true,"verbs":["get","patch"]}]}`)
	case r.URL.Path == "/apis/broken.example.com/v1":
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `{"message":"the server is currently unable to handle the request"}`)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/deployments"):
		io.WriteString(w, `{"items":[{"metadata":{"name":"web","namespace":"shop"}}]}`)
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pods/missing"):
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"kind":"Status","message":"pods \"missing\" not found","reason":"NotFound"}`)
	case strings.HasSuffix(r.URL.Path, "/log"):
		w.Header().Set("Content-Type", "text/plain")
		io.WriteString(w, "2026-10-12T10:00:00Z line one\n2026-10-12T10:00:01Z line two\n")
	default:
		io.WriteString(w, `{}`)
	}
}

func newTestClient(t *testing.T) (*Client, *fakeAPI) {
	t.Helper()
	api := &fakeAPI{}
	server := httptest.NewTLSServer(api)
	t.Cleanup(server.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewClient(Config{Server: server.URL, CAData: ca, Token: "sa-token"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, api
}

func TestClientAuthenticatesAndTrustsOnlyTheConfiguredCA(t *testing.T) {
	client, api := newTestClient(t)
	version, err := client.Version(context.Background())
	if err != nil || version != "v1.31.4+k3s1" {
		t.Fatalf("Version = %q, %v", version, err)
	}
	if got := api.last().Authorization; got != "Bearer sa-token" {
		t.Errorf("Authorization = %q", got)
	}
	// Without the CA, the self-signed server must be rejected, not trusted.
	untrusting, _ := NewClient(Config{Server: client.Server(), Token: "sa-token"})
	if _, err := untrusting.Version(context.Background()); err == nil {
		t.Error("a server signed by an unknown CA must not be trusted")
	}
}

func TestDiscoverySkipsSubresourcesUnlistablesAndBrokenGroups(t *testing.T) {
	client, _ := newTestClient(t)
	resources, err := client.Resources(context.Background())
	if err != nil {
		t.Fatalf("Resources: %v", err)
	}
	names := []string{}
	for _, resource := range resources {
		names = append(names, resource.Group+"/"+resource.Name)
	}
	// Sorted by group then name; "pods/log", "bindings" (not listable) and the
	// failing aggregated group are left out.
	if got, want := strings.Join(names, " "), "/namespaces /pods apps/deployments"; got != want {
		t.Fatalf("resources = %q, want %q", got, want)
	}
}

func TestResourcePaths(t *testing.T) {
	pods := Resource{Version: "v1", Name: "pods", Namespaced: true}
	deployments := Resource{Group: "apps", Version: "v1", Name: "deployments", Namespaced: true}
	nodes := Resource{Version: "v1", Name: "nodes"}
	cases := map[string]string{
		pods.Path("", ""):                   "/api/v1/pods",
		pods.Path("shop", ""):               "/api/v1/namespaces/shop/pods",
		pods.Path("shop", "web-1"):          "/api/v1/namespaces/shop/pods/web-1",
		deployments.Path("shop", "web"):     "/apis/apps/v1/namespaces/shop/deployments/web",
		nodes.Path("ignored-namespace", ""): "/api/v1/nodes",
		pods.Path("a/b", "x y"):             "/api/v1/namespaces/a%2Fb/pods/x%20y",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("path = %q, want %q", got, want)
		}
	}
	if deployments.APIVersion() != "apps/v1" || pods.APIVersion() != "v1" {
		t.Error("APIVersion rendered incorrectly")
	}
}

func TestListRestoresKindAndGetReportsNotFound(t *testing.T) {
	client, _ := newTestClient(t)
	deployments := Resource{Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	items, err := client.List(context.Background(), deployments, "shop")
	if err != nil || len(items) != 1 {
		t.Fatalf("List = %v, %v", items, err)
	}
	if items[0]["kind"] != "Deployment" || items[0]["apiVersion"] != "apps/v1" {
		t.Errorf("list items must carry apiVersion and kind: %v", items[0])
	}
	pods := Resource{Version: "v1", Name: "pods", Kind: "Pod", Namespaced: true}
	_, err = client.Get(context.Background(), pods, "shop", "missing")
	if !IsNotFound(err) || !strings.Contains(err.Error(), `pods "missing" not found`) {
		t.Errorf("expected the API server's not-found message, got %v", err)
	}
}

const manifest = `
apiVersion: v1
kind: Namespace
metadata:
  name: shop
---
# an empty document follows
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 2
`

func TestApplyUsesServerSideApplyAndDefaultsTheNamespace(t *testing.T) {
	client, api := newTestClient(t)
	resources, _ := client.Resources(context.Background())

	applied, err := client.Apply(context.Background(), resources, []byte(manifest), "shop")
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(applied) != 2 || applied[0].Kind != "Namespace" || applied[1].Namespace != "shop" {
		t.Fatalf("applied = %+v", applied)
	}
	request := api.last()
	if request.Method != http.MethodPatch || request.Path != "/apis/apps/v1/namespaces/shop/deployments/web" {
		t.Errorf("unexpected request: %s %s", request.Method, request.Path)
	}
	if request.ContentType != "application/apply-patch+yaml" || !strings.Contains(request.Query, "fieldManager=pier") ||
		!strings.Contains(request.Query, "force=true") {
		t.Errorf("not a server-side apply: %s ?%s", request.ContentType, request.Query)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(request.Body), &sent); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if sent["metadata"].(map[string]any)["namespace"] != "shop" {
		t.Errorf("the default namespace must be written into the object: %v", sent["metadata"])
	}
	// The cluster-scoped Namespace must not be sent under a namespace path.
	api.mu.Lock()
	first := api.requests[len(api.requests)-2]
	api.mu.Unlock()
	if first.Path != "/api/v1/namespaces/shop" {
		t.Errorf("namespace applied at %s", first.Path)
	}
}

func TestApplyRejectsUnknownKindsAndBrokenManifests(t *testing.T) {
	client, _ := newTestClient(t)
	resources, _ := client.Resources(context.Background())
	cases := map[string]string{
		"unknown kind": "apiVersion: example.com/v1\nkind: Widget\nmetadata:\n  name: x\n",
		"missing kind": "apiVersion: v1\nmetadata:\n  name: x\n",
		"missing name": "apiVersion: v1\nkind: Namespace\nmetadata: {}\n",
		"invalid yaml": "apiVersion: v1\n  kind: [",
		"empty":        "\n---\n",
	}
	for name, body := range cases {
		if _, err := client.Apply(context.Background(), resources, []byte(body), "default"); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestScaleRestartDeleteAndLogs(t *testing.T) {
	client, api := newTestClient(t)
	deployments := Resource{Group: "apps", Version: "v1", Name: "deployments", Kind: "Deployment", Namespaced: true}
	ctx := context.Background()

	if err := client.Scale(ctx, deployments, "shop", "web", 3); err != nil {
		t.Fatalf("Scale: %v", err)
	}
	if request := api.last(); request.Path != "/apis/apps/v1/namespaces/shop/deployments/web/scale" ||
		request.ContentType != "application/merge-patch+json" || request.Body != `{"spec":{"replicas":3}}` {
		t.Errorf("unexpected scale request: %+v", request)
	}
	if err := client.Restart(ctx, deployments, "shop", "web"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if request := api.last(); !strings.Contains(request.Body, "pier.io/restartedAt") || strings.HasSuffix(request.Path, "/scale") {
		t.Errorf("unexpected restart request: %+v", request)
	}
	if err := client.Delete(ctx, deployments, "shop", "web"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if request := api.last(); request.Method != http.MethodDelete || !strings.Contains(request.Query, "propagationPolicy=Background") {
		t.Errorf("unexpected delete request: %+v", request)
	}
	stream, err := client.PodLogs(ctx, "shop", "web-1", "app", 50, false)
	if err != nil {
		t.Fatalf("PodLogs: %v", err)
	}
	defer stream.Close()
	logs, _ := io.ReadAll(stream)
	if !strings.Contains(string(logs), "line two") {
		t.Errorf("logs = %q", logs)
	}
	if request := api.last(); request.Path != "/api/v1/namespaces/shop/pods/web-1/log" ||
		!strings.Contains(request.Query, "tailLines=50") || !strings.Contains(request.Query, "container=app") {
		t.Errorf("unexpected log request: %+v", request)
	}
}

func kubeconfig(user string) string {
	ca := base64.StdEncoding.EncodeToString([]byte("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"))
	return `apiVersion: v1
kind: Config
current-context: prod
clusters:
  - name: c1
    cluster:
      server: https://10.0.0.5:6443/
      certificate-authority-data: ` + ca + `
  - name: c2
    cluster:
      server: https://staging.example.com
      insecure-skip-tls-verify: true
contexts:
  - name: prod
    context: {cluster: c1, user: u1}
  - name: staging
    context: {cluster: c2, user: u1}
users:
  - name: u1
    user:
` + user
}

func TestParseKubeconfig(t *testing.T) {
	config, err := ParseKubeconfig([]byte(kubeconfig("      token: abc123\n")), "")
	if err != nil {
		t.Fatalf("ParseKubeconfig: %v", err)
	}
	if config.Server != "https://10.0.0.5:6443" || config.Token != "abc123" || len(config.CAData) == 0 || config.Insecure {
		t.Errorf("current context misread: %+v", config)
	}
	staging, err := ParseKubeconfig([]byte(kubeconfig("      token: abc123\n")), "staging")
	if err != nil || staging.Server != "https://staging.example.com" || !staging.Insecure {
		t.Errorf("explicit context misread: %+v, %v", staging, err)
	}
	names, current, err := Contexts([]byte(kubeconfig("      token: x\n")))
	if err != nil || current != "prod" || strings.Join(names, ",") != "prod,staging" {
		t.Errorf("Contexts = %v, %q, %v", names, current, err)
	}
}

// A kubeconfig is stored and used on the Pier host: credentials that live in
// someone's files or behind a local command cannot work and must be refused
// with an explanation instead of failing later with a cryptic 401.
func TestParseKubeconfigRejectsCredentialsPierCannotUse(t *testing.T) {
	unsupported := map[string]string{
		"exec plugin (GKE/EKS)": "      exec:\n        command: gke-gcloud-auth-plugin\n",
		"auth provider":         "      auth-provider:\n        name: gcp\n",
		"client cert file":      "      client-certificate: /home/me/.minikube/client.crt\n      client-key: /home/me/.minikube/client.key\n",
		"token file":            "      tokenFile: /var/run/secrets/token\n",
	}
	for name, user := range unsupported {
		if _, err := ParseKubeconfig([]byte(kubeconfig(user)), ""); !errors.Is(err, ErrUnsupportedAuth) {
			t.Errorf("%s: expected ErrUnsupportedAuth, got %v", name, err)
		}
	}
	invalid := map[string]string{
		"no credentials":  "      {}\n",
		"unknown context": "      token: x\n",
		"bad base64":      "      client-certificate-data: '!!!'\n      client-key-data: '!!!'\n",
	}
	for name, user := range invalid {
		context := ""
		if name == "unknown context" {
			context = "nope"
		}
		if _, err := ParseKubeconfig([]byte(kubeconfig(user)), context); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestRewriteServerPointsEveryClusterAtTheNewAddress(t *testing.T) {
	rewritten, err := RewriteServer([]byte(kubeconfig("      token: abc\n")), "https://203.0.113.9:16443")
	if err != nil {
		t.Fatalf("RewriteServer: %v", err)
	}
	config, err := ParseKubeconfig(rewritten, "")
	if err != nil || config.Server != "https://203.0.113.9:16443" || config.Token != "abc" {
		t.Errorf("rewritten kubeconfig misread: %+v, %v", config, err)
	}
}

func TestEncodeYAMLDropsManagedFields(t *testing.T) {
	rendered, err := EncodeYAML(Object{"kind": "Pod", "metadata": map[string]any{"name": "p", "managedFields": []any{"noise"}}})
	if err != nil || strings.Contains(rendered, "managedFields") || !strings.Contains(rendered, "name: p") {
		t.Errorf("EncodeYAML = %q, %v", rendered, err)
	}
}
