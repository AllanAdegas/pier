package cluster

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"pier/internal/kube"
	"pier/internal/platform/apperr"
	"pier/internal/platform/executor"
	"pier/internal/platform/oplog"
	"pier/internal/platform/secretbox"
	"pier/internal/platform/sqlite"
	"pier/internal/server"
)

type fakeConnector struct{ ex *executor.Fake }

func (f fakeConnector) Executor(context.Context, string) (executor.Executor, server.Server, error) {
	return f.ex, server.Server{ID: "srv1", Kind: server.KindSSH, Host: "203.0.113.9", DataDir: "/data/pier"}, nil
}

// apiServer answers just enough of the Kubernetes API for the service.
func apiServer(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/version":
			io.WriteString(w, `{"gitVersion":"v1.36.5+k3s1"}`)
		case "/api/v1/nodes":
			io.WriteString(w, `{"items":[{"metadata":{"name":"n1"}},{"metadata":{"name":"n2"}}]}`)
		default:
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"message":"not found"}`)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func kubeconfigFor(server *httptest.Server) string {
	ca := base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	return `apiVersion: v1
kind: Config
current-context: target
clusters:
  - name: target
    cluster: {server: ` + server.URL + `, certificate-authority-data: ` + ca + `}
  - name: other
    cluster: {server: https://other.example.com}
contexts:
  - name: target
    context: {cluster: target, user: target-user}
  - name: other
    context: {cluster: other, user: other-user}
users:
  - name: target-user
    user: {token: target-token}
  - name: other-user
    user: {token: OTHER-CLUSTER-SECRET}
`
}

func newService(t *testing.T) (*Service, *executor.Fake) {
	t.Helper()
	db, err := sqlite.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`INSERT INTO servers (id, name, kind, data_dir, created_at)
		VALUES ('srv1', 'test', 'ssh', '/data/pier', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	box, _ := secretbox.New("test-passphrase-123456")
	fake := executor.NewFake()
	service := NewService(db, box, fakeConnector{fake}, oplog.NewRegistry(), "127.0.0.1")
	// No real network in tests: every port is free unless a test says otherwise.
	service.portInUse = func(context.Context, string) bool { return false }
	return service, fake
}

func TestImportChecksTheClusterAndKeepsOnlyTheSelectedContext(t *testing.T) {
	service, _ := newService(t)
	api := apiServer(t)

	cluster, err := service.Import(context.Background(), ImportInput{Name: "prod", Kubeconfig: kubeconfigFor(api)})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if cluster.Status != StatusReady || cluster.Version != "v1.36.5+k3s1" || cluster.Nodes != 2 || cluster.Endpoint != api.URL {
		t.Errorf("cluster not checked on import: %+v", cluster)
	}
	stored, err := service.Kubeconfig(context.Background(), cluster.ID)
	if err != nil {
		t.Fatalf("Kubeconfig: %v", err)
	}
	// The file had a second cluster with its own token; it must not be kept.
	if strings.Contains(stored, "OTHER-CLUSTER-SECRET") || strings.Contains(stored, "other.example.com") {
		t.Errorf("credentials of an unrelated cluster were stored:\n%s", stored)
	}
	if config, err := kube.ParseKubeconfig([]byte(stored), ""); err != nil || config.Token != "target-token" {
		t.Errorf("stored kubeconfig is not usable: %+v, %v", config, err)
	}
	var sealed string
	if err := service.db.QueryRow(`SELECT kubeconfig FROM clusters WHERE id = ?`, cluster.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sealed, "target-token") {
		t.Error("the kubeconfig must be encrypted at rest")
	}
}

func TestImportRegistersAnUnreachableClusterAsSuch(t *testing.T) {
	service, _ := newService(t)
	api := apiServer(t)
	config := kubeconfigFor(api)
	api.Close()

	cluster, err := service.Import(context.Background(), ImportInput{Name: "down", Kubeconfig: config})
	if err != nil {
		t.Fatalf("an unreachable cluster may be temporarily down and must still be registered: %v", err)
	}
	if cluster.Status != StatusUnreachable || cluster.StatusMessage == "" {
		t.Errorf("expected unreachable with a reason, got %+v", cluster)
	}
}

func TestImportRejectsWhatCannotWork(t *testing.T) {
	service, _ := newService(t)
	gke := `apiVersion: v1
current-context: gke
clusters: [{name: gke, cluster: {server: https://34.1.2.3}}]
contexts: [{name: gke, context: {cluster: gke, user: gke}}]
users: [{name: gke, user: {exec: {command: gke-gcloud-auth-plugin}}}]`
	cases := map[string]ImportInput{
		"name":       {Kubeconfig: gke},
		"kubeconfig": {Name: "x", Kubeconfig: gke},
	}
	for field, input := range cases {
		_, err := service.Import(context.Background(), input)
		appErr, ok := apperr.As(err)
		if !ok || appErr.Fields[field] == "" {
			t.Errorf("expected a validation error on %q, got %v", field, err)
		}
	}
	_, err := service.Import(context.Background(), ImportInput{Name: "x", Kubeconfig: gke})
	if appErr, _ := apperr.As(err); !strings.Contains(appErr.Fields["kubeconfig"], "ServiceAccount") {
		t.Errorf("the message must tell how to proceed with exec-based kubeconfigs: %v", err)
	}
}

func TestK3sComposeRunsAPrivilegedNodeWithPersistentState(t *testing.T) {
	cluster := Cluster{ID: "clu1", Name: "Dev cluster", K3s: K3sConfig{Image: DefaultK3sImage, APIPort: 16443, HTTPPort: 18080}}
	remote := server.Server{Kind: server.KindSSH, Host: "203.0.113.9"}
	rendered, err := k3sCompose(cluster, remote.Host).Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	text := string(rendered)
	for _, expected := range []string{
		// Readable container name; the volume and the node name keep the ID.
		"image: " + DefaultK3sImage, "container_name: dev-cluster-clu1", "privileged: true", "clu1_data:/var/lib/rancher/k3s",
		"pier-clu1",
		"16443:6443", "18080:80", "pier.type: cluster", "- 203.0.113.9",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("compose misses %q:\n%s", expected, text)
		}
	}
}

// In a container the loopback is the container itself, so the address of the
// local server comes from the configuration.
func TestNodeAddressOfTheLocalServerIsConfigurable(t *testing.T) {
	service := &Service{localAddress: "host.docker.internal"}
	local := server.Server{Kind: server.KindLocal}
	remote := server.Server{Kind: server.KindSSH, Host: "203.0.113.9"}
	if service.nodeAddress(local) != "host.docker.internal" || service.nodeAddress(remote) != "203.0.113.9" {
		t.Error("node address must be the configured one for the local server and the host for remote ones")
	}
}

func TestK3sInputValidation(t *testing.T) {
	valid := K3sInput{Name: "dev", ServerID: "srv1"}
	if err := valid.validate(); err != nil {
		t.Fatalf("defaults must be valid: %v", err)
	}
	if valid.Image != DefaultK3sImage || valid.APIPort != 6443 || valid.HTTPPort != 8080 {
		t.Errorf("defaults not applied: %+v", valid)
	}
	cases := map[string]K3sInput{
		"name":      {ServerID: "srv1"},
		"server_id": {Name: "x"},
		"image":     {Name: "x", ServerID: "srv1", Image: "evil/k3s:latest"},
		"api_port":  {Name: "x", ServerID: "srv1", APIPort: 80},
		"http_port": {Name: "x", ServerID: "srv1", APIPort: 7000, HTTPPort: 7000},
	}
	for field, input := range cases {
		appErr, ok := apperr.As(input.validate())
		if !ok || appErr.Fields[field] == "" {
			t.Errorf("expected a validation error on %q, got %v", field, input.validate())
		}
	}
}

func TestDeleteDestroysK3sButOnlyUnregistersImportedClusters(t *testing.T) {
	service, fake := newService(t)
	ctx := context.Background()
	api := apiServer(t)
	imported, _ := service.Import(ctx, ImportInput{Name: "prod", Kubeconfig: kubeconfigFor(api)})
	if err := service.Delete(ctx, imported.ID, false); err != nil {
		t.Fatalf("Delete(imported): %v", err)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("unregistering an imported cluster must not touch any server: %v", fake.Commands)
	}

	owned := Cluster{ID: "k3sclusterid01", Name: "dev", Source: SourceK3s, ServerID: "srv1", Status: StatusReady}
	if err := service.insert(ctx, owned, "kubeconfig"); err != nil {
		t.Fatal(err)
	}
	fake.On("docker ps", "cid\tk3sclusterid01\trancher/k3s\trunning\tUp\tcluster\tk3sclusterid01\t", 0)
	if err := service.Delete(ctx, owned.ID, false); err != nil {
		t.Fatalf("Delete(k3s): %v", err)
	}
	for _, expected := range []string{"docker rm -f cid", "docker volume rm --force k3sclusterid01_data", "rm -rf /data/pier/clusters/k3sclusterid01"} {
		if !fake.Ran(expected) {
			t.Errorf("expected %q; ran:\n%s", expected, strings.Join(fake.Commands, "\n"))
		}
	}
	if _, err := service.Get(ctx, owned.ID); err == nil {
		t.Error("the record must be gone")
	}
}

func TestSummarize(t *testing.T) {
	cases := []struct {
		kind           string
		object         string
		status, health string
		info           string
	}{
		{"Pod", `{status: {phase: Running, containerStatuses: [{ready: true, restartCount: 0}]}, spec: {nodeName: n1}}`, "Running · 1/1", "ok", "n1"},
		{"Pod", `{status: {phase: Running, containerStatuses: [{ready: false, restartCount: 4, state: {waiting: {reason: CrashLoopBackOff}}}]}}`, "CrashLoopBackOff", "error", "· 4 reinício(s)"},
		{"Pod", `{status: {phase: Pending, containerStatuses: [{ready: false, state: {waiting: {reason: ContainerCreating}}}]}}`, "Pending · 0/1", "progressing", ""},
		{"Pod", `{status: {phase: Succeeded}}`, "Concluído", "ok", ""},
		{"Deployment", `{spec: {replicas: 3, template: {spec: {containers: [{image: "nginx:1.27"}]}}}, status: {readyReplicas: 3}}`, "3/3 prontos", "ok", "nginx:1.27"},
		{"Deployment", `{spec: {replicas: 3}, status: {readyReplicas: 1}}`, "1/3 prontos", "progressing", ""},
		{"Deployment", `{spec: {replicas: 0}}`, "Escalado para zero", "", ""},
		{"StatefulSet", `{spec: {}, status: {readyReplicas: 1}}`, "1/1 prontos", "ok", ""},
		{"DaemonSet", `{status: {desiredNumberScheduled: 2, numberReady: 1}}`, "1/2 prontos", "progressing", ""},
		{"Job", `{status: {failed: 1}}`, "Falhou", "error", ""},
		{"Node", `{metadata: {labels: {"node-role.kubernetes.io/control-plane": "true"}}, status: {conditions: [{type: Ready, status: "True"}], nodeInfo: {kubeletVersion: v1.36.5+k3s1}}}`, "Ready", "ok", "v1.36.5+k3s1 · control-plane"},
		{"Node", `{status: {conditions: [{type: Ready, status: "False"}]}}`, "NotReady", "error", "·"},
		{"Namespace", `{status: {phase: Active}}`, "Active", "ok", ""},
		{"PersistentVolumeClaim", `{spec: {storageClassName: local-path}, status: {phase: Bound, capacity: {storage: 1Gi}}}`, "Bound", "ok", "1Gi · local-path"},
		{"Service", `{spec: {type: ClusterIP, clusterIP: 10.43.0.1, ports: [{port: 80}]}}`, "ClusterIP", "", "10.43.0.1 · 80/TCP"},
		{"Ingress", `{spec: {ingressClassName: traefik, rules: [{host: a.example.com}, {host: b.example.com}]}}`, "traefik", "", "a.example.com, b.example.com"},
		{"Event", `{type: Warning, reason: BackOff, message: "restarting", involvedObject: {kind: Pod, name: web-1}}`, "Warning", "error", "BackOff Pod/web-1: restarting"},
		{"Widget", `{spec: {anything: true}}`, "", "", ""},
	}
	for _, tc := range cases {
		var object map[string]any
		if err := yaml.Unmarshal([]byte(tc.object), &object); err != nil {
			t.Fatalf("%s: bad fixture: %v", tc.kind, err)
		}
		// Kubernetes sends JSON, where every number is a float64.
		row := summarize(tc.kind, floats(object).(map[string]any))
		if row.Status != tc.status || row.Health != tc.health || row.Info != tc.info {
			t.Errorf("%s %s\n got status=%q health=%q info=%q\nwant status=%q health=%q info=%q",
				tc.kind, tc.object, row.Status, row.Health, row.Info, tc.status, tc.health, tc.info)
		}
	}
}

func floats(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			typed[key] = floats(item)
		}
	case []any:
		for index, item := range typed {
			typed[index] = floats(item)
		}
	case int:
		return float64(typed)
	}
	return value
}

// Listing Secrets must expose how many keys they hold, never the values.
func TestSecretSummaryNeverCarriesValues(t *testing.T) {
	row := summarize("Secret", kube.Object{
		"metadata": map[string]any{"name": "db"}, "type": "Opaque",
		"data": map[string]any{"password": "c3VwZXJzZWNyZXQ=", "user": "YWRtaW4="},
	})
	if row.Info != "2 chave(s)" || row.Status != "Opaque" {
		t.Errorf("unexpected summary: %+v", row)
	}
	if strings.Contains(row.Info+row.Status+row.Name, "c3VwZXJzZWNyZXQ") {
		t.Error("secret value leaked into the listing")
	}
}

func TestWorkloadManifest(t *testing.T) {
	spec := WorkloadSpec{Name: "web", Namespace: "shop", Image: "nginx:1.27", Replicas: 2, Port: 80,
		Env: map[string]string{"B": "2", "A": "1"}, Host: "Shop.Example.com"}
	if err := spec.validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	rendered, err := spec.manifest()
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	objects, err := kube.DecodeManifest(rendered)
	if err != nil {
		t.Fatalf("the generated manifest must be valid: %v\n%s", err, rendered)
	}
	kinds := []string{}
	for _, object := range objects {
		kinds = append(kinds, object["kind"].(string))
	}
	if got := strings.Join(kinds, ","); got != "Namespace,Deployment,Service,Ingress" {
		t.Fatalf("kinds = %s", got)
	}
	text := string(rendered)
	for _, expected := range []string{"replicas: 2", "image: nginx:1.27", "containerPort: 80", "host: shop.example.com", "name: A"} {
		if !strings.Contains(text, expected) {
			t.Errorf("manifest misses %q:\n%s", expected, text)
		}
	}
	if strings.Index(text, "name: A") > strings.Index(text, "name: B") {
		t.Error("environment must be rendered in a stable order")
	}

	minimal := WorkloadSpec{Name: "job", Image: "busybox"}
	_ = minimal.validate()
	rendered, _ = minimal.manifest()
	if objects, _ := kube.DecodeManifest(rendered); len(objects) != 1 {
		t.Errorf("without port, host or namespace only the Deployment is expected, got %d objects", len(objects))
	}
}

func TestWorkloadValidation(t *testing.T) {
	cases := map[string]WorkloadSpec{
		"name":      {Name: "Web_App", Image: "nginx"},
		"namespace": {Name: "web", Namespace: "My Space", Image: "nginx"},
		"image":     {Name: "web", Image: "nginx; id"},
		"replicas":  {Name: "web", Image: "nginx", Replicas: 500},
		"port":      {Name: "web", Image: "nginx", Host: "a.example.com"},
		"host":      {Name: "web", Image: "nginx", Port: 80, Host: "a.example.com/path"},
		"env":       {Name: "web", Image: "nginx", Env: map[string]string{"bad-key": "x"}},
	}
	for field, spec := range cases {
		appErr, ok := apperr.As(spec.validate())
		if !ok || appErr.Fields[field] == "" {
			t.Errorf("expected a validation error on %q, got %v", field, spec.validate())
		}
	}
}

// Publishing a cluster on a port another service answers on would make Pier
// talk to that service; the request is refused before anything is started.
func TestCreateK3sRefusesPortsAlreadyInUse(t *testing.T) {
	service, fake := newService(t)
	probed := []string{}
	service.portInUse = func(_ context.Context, address string) bool {
		probed = append(probed, address)
		return strings.HasSuffix(address, ":8700")
	}
	_, err := service.CreateK3s(context.Background(), K3sInput{Name: "dev", ServerID: "srv1", APIPort: 8700, HTTPPort: 18080})
	appErr, ok := apperr.As(err)
	if !ok || !strings.Contains(appErr.Fields["api_port"], "já está em uso em 203.0.113.9") || appErr.Fields["http_port"] != "" {
		t.Fatalf("expected a validation error only on api_port, got %v", err)
	}
	if len(probed) != 2 {
		t.Errorf("both ports must be probed on the node address, probed %v", probed)
	}
	if len(fake.Commands) != 0 {
		t.Errorf("nothing must be started on the server: %v", fake.Commands)
	}
	if clusters, _ := service.List(context.Background()); len(clusters) != 0 {
		t.Errorf("no cluster must be recorded: %+v", clusters)
	}
}

func TestDeleteCancelsACreationInProgress(t *testing.T) {
	service, fake := newService(t)
	ctx := context.Background()
	// The kubeconfig never shows up, so provisioning keeps waiting.
	fake.On("docker exec", "cat: can't open '/etc/rancher/k3s/k3s.yaml'", 1)
	cluster, err := service.CreateK3s(ctx, K3sInput{Name: "dev", ServerID: "srv1"})
	if err != nil {
		t.Fatalf("CreateK3s: %v", err)
	}
	if cluster.Status != StatusProvisioning || !service.Operation(cluster.ID).Running {
		t.Fatalf("creation should be in progress: %+v", cluster)
	}
	if err := service.Delete(ctx, cluster.ID, false); err != nil {
		t.Fatalf("a cluster being created must be removable: %v", err)
	}
	if _, err := service.Get(ctx, cluster.ID); err == nil {
		t.Error("the record must be gone")
	}
	if !fake.Ran("docker volume rm --force " + cluster.ID + "_data") {
		t.Errorf("the half-created node must be destroyed; ran:\n%s", strings.Join(fake.Commands, "\n"))
	}
}

func TestRecoverProvisioningAfterRestart(t *testing.T) {
	service, _ := newService(t)
	ctx := context.Background()
	stuck := Cluster{ID: "stuckcluster001", Name: "x", Source: SourceK3s, ServerID: "srv1", Status: StatusProvisioning}
	if err := service.insert(ctx, stuck, ""); err != nil {
		t.Fatal(err)
	}
	if err := service.RecoverProvisioning(ctx); err != nil {
		t.Fatalf("RecoverProvisioning: %v", err)
	}
	recovered, _ := service.Get(ctx, stuck.ID)
	if recovered.Status != StatusUnreachable || !strings.Contains(recovered.StatusMessage, "interrompida") {
		t.Errorf("a creation orphaned by a restart must be flagged: %+v", recovered)
	}
}
