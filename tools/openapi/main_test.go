package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	if err := os.Chdir("../.."); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}
func loadDocument(t *testing.T) M {
	t.Helper()
	data, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	var doc M
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}
func op(t *testing.T, doc M, path, method string) M {
	t.Helper()
	paths := doc["paths"].(map[string]any)
	v, ok := paths[path]
	if !ok {
		t.Fatalf("missing %s", path)
	}
	operation, ok := v.(map[string]any)[method]
	if !ok {
		t.Fatalf("missing %s %s", method, path)
	}
	return operation.(map[string]any)
}

func TestCheckedInContractCoversCurrentRegistrations(t *testing.T) {
	actual, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile("api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(actual) != string(expected) {
		t.Fatal("contract drift: run go run ./tools/openapi")
	}
	doc := loadDocument(t)
	count := 0
	ids := map[string]bool{}
	for _, value := range doc["paths"].(map[string]any) {
		for _, v := range value.(map[string]any) {
			count++
			o := v.(map[string]any)
			id := o["operationId"].(string)
			if ids[id] {
				t.Fatal("duplicate operation ID", id)
			}
			ids[id] = true
		}
	}
	if count != len(routes) || count < 280 {
		t.Fatalf("missing route inventory: %d vs %d", count, len(routes))
	}
	for _, path := range []string{"/api/v1/applications/{id}/previews", "/api/v1/stacks/{id}/volume-backups", "/api/v1/servers/{id}/docker/images", "/api/v1/projects/{id}/shared-variables", "/api/v1/environments/{id}/shared-variables", "/api/v1/applications/{id}/git-provider", "/api/v1/stacks/{id}/git-provider", "/webhooks/git-provider/github", "/api/v1/applications/{id}/placement"} {
		if doc["paths"].(map[string]any)[path] == nil {
			t.Error("missing dynamic/feature route", path)
		}
	}
	walkJSON(doc, func(m map[string]any) {
		if ref, ok := m["$ref"].(string); ok {
			if !strings.HasPrefix(ref, "#/components/schemas/") {
				t.Error("unexpected ref", ref)
				return
			}
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			if doc["components"].(map[string]any)["schemas"].(map[string]any)[name] == nil {
				t.Error("dangling schema", name)
			}
		}
	})
}
func walkJSON(v any, fn func(map[string]any)) {
	switch x := v.(type) {
	case map[string]any:
		fn(x)
		for _, v := range x {
			walkJSON(v, fn)
		}
	case []any:
		for _, v := range x {
			walkJSON(v, fn)
		}
	}
}

func TestInventoryDetectsNewRoutesAndUnresolvedPatterns(t *testing.T) {
	reset()
	if err := parseRepo(); err != nil {
		t.Fatal(err)
	}
	addFunction(t, `func (a *API) fixture(mux *http.ServeMux) { a.route(mux,"POST /api/v1/fixture-new",canWrite,a.createApplication) }`)
	scanRoutes()
	found := false
	for _, r := range routes {
		if r.Pattern == "POST /api/v1/fixture-new" {
			found = true
		}
	}
	if !found {
		t.Fatal("new registration was silently omitted")
	}
	routes = nil
	unresolved = nil
	addFunction(t, `func (a *API) fixture(mux *http.ServeMux) { a.route(mux,unknownPrefix+"/hidden",canWrite,a.createApplication) }`)
	scanRoutes()
	if len(unresolved) == 0 {
		t.Fatal("unknown dynamic registration must fail closed")
	}
}
func addFunction(t *testing.T, body string) {
	t.Helper()
	f, err := parser.ParseFile(fs, "fixture.go", "package httpapi\n"+body, 0)
	if err != nil {
		t.Fatal(err)
	}
	decl := f.Decls[0].(*ast.FuncDecl)
	funcs["httpapi.API.fixture"] = fun{decl, scope{pkg: "httpapi"}}
}
func TestRequestsResponsesAndTransportSecurity(t *testing.T) {
	doc := loadDocument(t)
	create := op(t, doc, "/api/v1/applications", "post")
	if create["x-pier-ability"] != "write" {
		t.Fatal("wrong create ability")
	}
	rb := create["requestBody"].(map[string]any)
	if rb["required"] != true {
		t.Fatal("create must decode JSON")
	}
	sc := rb["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if sc["$ref"] != "#/components/schemas/application.CreateInput" {
		t.Fatalf("untyped create input: %v", sc)
	}
	deploy := op(t, doc, "/api/v1/applications/{id}/deploy", "post")
	if deploy["requestBody"].(map[string]any)["required"] != false {
		t.Fatal("optional deploy body became required")
	}
	get := op(t, doc, "/api/v1/applications/{id}", "get")
	resp := get["responses"].(map[string]any)["200"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)
	if resp["$ref"] != "#/components/schemas/application.Application" {
		t.Fatalf("untyped app response %v", resp)
	}
	enroll := op(t, doc, "/api/v1/me/2fa/enroll", "post")
	if enroll["x-pier-session-only"] != true || len(enroll["security"].([]any)) != 1 {
		t.Fatal("2FA must require session")
	}
	webhook := op(t, doc, "/webhooks/git-provider/github", "post")
	if len(webhook["security"].([]any)) != 0 || !strings.Contains(webhook["description"].(string), "signature") {
		t.Fatal("webhook must describe provider auth")
	}
	terminal := op(t, doc, "/api/v1/terminal", "get")
	if terminal["responses"].(map[string]any)["101"] == nil {
		t.Fatal("missing websocket response")
	}
	env := op(t, doc, "/api/v1/applications/{id}/envs", "get")
	if env["x-pier-sensitive"] != true {
		t.Fatal("sensitive-read restriction lost")
	}
}
func TestGenerationIsDeterministic(t *testing.T) {
	a, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generate()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("nondeterministic output")
	}
}
