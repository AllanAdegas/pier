package provision

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

const testToken = "test-token-0123456789abcdef"

// testKey is a throwaway ed25519 public key (no private half exists anywhere).
const testKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIIUi9oKTyDI6gt21NJonVKHnIfi9SvJOChM0IgPcBOzT pier"

type recorded struct {
	method, path, auth string
	body               map[string]any
}

// fakeAPI answers by "METHOD path" and records every request.
func fakeAPI(t *testing.T, answers map[string]struct {
	status int
	body   string
}) (*httptest.Server, func() []recorded) {
	t.Helper()
	var mu sync.Mutex
	requests := []recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		entry := recorded{method: r.Method, path: r.URL.RequestURI(), auth: r.Header.Get("Authorization")}
		_ = json.Unmarshal(raw, &entry.body)
		mu.Lock()
		requests = append(requests, entry)
		mu.Unlock()
		key := r.Method + " " + r.URL.Path
		answer, found := answers[key]
		if !found {
			answer = answers[r.Method+" "+r.URL.RequestURI()]
		}
		if answer.status == 0 {
			answer.status = http.StatusNotFound
		}
		w.WriteHeader(answer.status)
		_, _ = io.WriteString(w, answer.body)
	}))
	t.Cleanup(server.Close)
	return server, func() []recorded { mu.Lock(); defer mu.Unlock(); return append([]recorded{}, requests...) }
}

type answer = struct {
	status int
	body   string
}

func TestHetznerClientSpeaksTheCloudAPI(t *testing.T) {
	api, requests := fakeAPI(t, map[string]answer{
		"GET /locations":     {200, `{"locations":[{"name":"fsn1","description":"Falkenstein DC Park 1","city":"Falkenstein","country":"DE"}]}`},
		"GET /server_types":  {200, `{"server_types":[{"name":"cx22","cores":2,"memory":4,"disk":40,"architecture":"x86"},{"name":"cx11","deprecated":true}]}`},
		"GET /images":        {200, `{"images":[{"name":"ubuntu-24.04","description":"Ubuntu 24.04","architecture":"x86"},{"name":"rocky-9","description":"Rocky Linux 9"}]}`},
		"GET /ssh_keys":      {200, `{"ssh_keys":[]}`},
		"POST /ssh_keys":     {201, `{"ssh_key":{"id":77}}`},
		"POST /servers":      {201, `{"server":{"id":42,"status":"initializing","public_net":{"ipv4":{"ip":"203.0.113.7"}}}}`},
		"GET /servers/42":    {200, `{"server":{"id":42,"status":"running","public_net":{"ipv4":{"ip":"203.0.113.7"}}}}`},
		"DELETE /servers/42": {200, `{"action":{"id":1}}`},
	})
	client := NewHetzner(api.URL, testToken, nil)
	ctx := context.Background()
	catalog, err := client.Catalog(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Regions) != 1 || catalog.Regions[0].ID != "fsn1" || len(catalog.Sizes) != 1 || catalog.Sizes[0].ID != "cx22" ||
		len(catalog.Images) != 1 || catalog.Images[0].ID != "ubuntu-24.04" {
		t.Fatalf("catalog %+v", catalog)
	}
	ref, err := client.EnsureKey(ctx, "pier-cred", authorizedKeyOnly(testKey))
	if err != nil || ref != "77" {
		t.Fatalf("key %q %v", ref, err)
	}
	instance, err := client.Create(ctx, CreateRequest{Name: "web-1", Region: "fsn1", Size: "cx22", Image: "ubuntu-24.04", UserData: "#cloud-config", KeyRef: ref})
	if err != nil || instance.ExternalID != "42" || instance.Running {
		t.Fatalf("create %+v %v", instance, err)
	}
	if instance, err = client.Get(ctx, "42"); err != nil || !instance.Running || instance.IPv4 != "203.0.113.7" {
		t.Fatalf("get %+v %v", instance, err)
	}
	if err := client.Delete(ctx, "42"); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(ctx, "43"); err != nil {
		t.Fatal("machine already gone must not fail:", err)
	}
	if _, err := client.Get(ctx, "../servers"); err == nil {
		t.Fatal("path injection accepted")
	}
	for _, request := range requests() {
		if request.auth != "Bearer "+testToken {
			t.Fatalf("missing token on %s %s", request.method, request.path)
		}
		if request.method == "POST" && request.path == "/servers" {
			if request.body["user_data"] != "#cloud-config" || request.body["ssh_keys"].([]any)[0].(float64) != 77 || request.body["location"] != "fsn1" {
				t.Fatalf("create body %+v", request.body)
			}
		}
		if request.method == "GET" && strings.HasPrefix(request.path, "/ssh_keys?fingerprint=") && !strings.Contains(request.path, "%3A") {
			t.Fatalf("fingerprint not escaped: %s", request.path)
		}
	}
}

func TestDigitalOceanClientSpeaksTheDropletsAPI(t *testing.T) {
	api, requests := fakeAPI(t, map[string]answer{
		"GET /regions":             {200, `{"regions":[{"slug":"nyc3","name":"New York 3","available":true},{"slug":"old1","available":false}]}`},
		"GET /sizes":               {200, `{"sizes":[{"slug":"s-1vcpu-1gb","memory":1024,"vcpus":1,"disk":25,"price_monthly":6,"available":true}]}`},
		"GET /images":              {200, `{"images":[{"slug":"ubuntu-24-04-x64","name":"24.04 (LTS) x64","distribution":"Ubuntu"},{"slug":"freebsd-14","distribution":"FreeBSD"}]}`},
		"POST /account/keys":       {201, `{"ssh_key":{"id":512}}`},
		"POST /droplets":           {202, `{"droplet":{"id":3164494,"status":"new","networks":{"v4":[]}}}`},
		"GET /droplets/3164494":    {200, `{"droplet":{"id":3164494,"status":"active","networks":{"v4":[{"ip_address":"10.0.0.2","type":"private"},{"ip_address":"198.51.100.4","type":"public"}]}}}`},
		"DELETE /droplets/3164494": {204, ``},
	})
	client := NewDigitalOcean(api.URL, testToken, nil)
	ctx := context.Background()
	catalog, err := client.Catalog(ctx)
	if err != nil || len(catalog.Regions) != 1 || len(catalog.Sizes) != 1 || len(catalog.Images) != 1 || catalog.Images[0].ID != "ubuntu-24-04-x64" {
		t.Fatalf("catalog %+v %v", catalog, err)
	}
	ref, err := client.EnsureKey(ctx, "pier-cred", authorizedKeyOnly(testKey))
	if err != nil || ref != "512" {
		t.Fatalf("key %q %v", ref, err)
	}
	instance, err := client.Create(ctx, CreateRequest{Name: "web-1", Region: "nyc3", Size: "s-1vcpu-1gb", Image: "ubuntu-24-04-x64", UserData: "#cloud-config", KeyRef: ref})
	if err != nil || instance.ExternalID != "3164494" {
		t.Fatalf("create %+v %v", instance, err)
	}
	if instance, err = client.Get(ctx, "3164494"); err != nil || !instance.Running || instance.IPv4 != "198.51.100.4" {
		t.Fatalf("get %+v %v", instance, err)
	}
	if err := client.Delete(ctx, "3164494"); err != nil {
		t.Fatal(err)
	}
	created := false
	for _, request := range requests() {
		if request.method == "POST" && request.path == "/droplets" {
			created = request.body["user_data"] == "#cloud-config" && request.body["region"] == "nyc3"
		}
	}
	if !created {
		t.Fatalf("droplet body %+v", requests())
	}
}

func TestProviderErrorsNeverCarryTheToken(t *testing.T) {
	api, _ := fakeAPI(t, map[string]answer{
		"GET /locations": {401, `{"error":{"code":"unauthorized","message":"invalid token"}}`},
		"GET /regions":   {500, `{"id":"server_error","message":"boom ` + testToken + "\x00\x1b[31m" + strings.Repeat("x", 500) + `"}`},
		"GET /sizes":     {429, `{}`},
	})
	if _, err := NewHetzner(api.URL, testToken, nil).Catalog(context.Background()); err == nil || !strings.Contains(err.Error(), "recusou o token") {
		t.Fatalf("401: %v", err)
	}
	_, err := NewDigitalOcean(api.URL, testToken, nil).Catalog(context.Background())
	if err == nil || strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "\x1b") || len(err.Error()) > 400 {
		t.Fatalf("500 leaked: %q", err)
	}
	unreachable := NewHetzner("http://127.0.0.1:1", testToken, nil)
	if _, err := unreachable.Catalog(context.Background()); err == nil || strings.Contains(err.Error(), testToken) {
		t.Fatalf("network error: %v", err)
	}
}

func TestCloudInitAuthorizesOnlyAValidatedKey(t *testing.T) {
	data, err := cloudInit(testKey)
	if err != nil || !strings.HasPrefix(data, "#cloud-config\n") || !strings.Contains(data, `- "ssh-ed25519 AAAA`) || strings.Contains(data, " pier\"") {
		t.Fatalf("%q %v", data, err)
	}
	for _, bad := range []string{"ssh-ed25519 AAAA\"\nruncmd: [rm]", "ecdsa-sha2 AAAA", "", "ssh-ed25519"} {
		if _, err := cloudInit(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
}
