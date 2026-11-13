package main

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func main() {
	check := flag.Bool("check", false, "fail when checked-in contract differs from current sources")
	out := flag.String("out", "api/openapi.json", "contract output path")
	flag.Parse()
	data, err := generate()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *check {
		old, err := os.ReadFile(*out)
		if err != nil || string(old) != string(data) {
			fmt.Fprintln(os.Stderr, "OpenAPI drift: run go run ./tools/openapi")
			os.Exit(1)
		}
		return
	}
	if err = os.MkdirAll(filepath.Dir(*out), 0755); err == nil {
		err = os.WriteFile(*out, data, 0644)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("OpenAPI: %d routes, %d schemas -> %s\n", len(routes), len(schemas), *out)
}

func generate() ([]byte, error) {
	reset()
	if err := parseRepo(); err != nil {
		return nil, err
	}
	scanRoutes()
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("unresolved route registrations: %v", unresolved)
	}
	paths := M{}
	seen := map[string]bool{}
	for _, r := range routes {
		parts := strings.SplitN(r.Pattern, " ", 2)
		method, path := strings.ToLower(parts[0]), parts[1]
		if seen[r.Pattern] {
			return nil, fmt.Errorf("duplicate route: %s", r.Pattern)
		}
		seen[r.Pattern] = true
		if paths[path] == nil {
			paths[path] = M{}
		}
		paths[path].(M)[method] = operation(r, method, path)
	}
	inventory := []string{}
	for _, r := range routes {
		inventory = append(inventory, r.Pattern+" "+r.Access)
	}
	sum := sha256.Sum256([]byte(strings.Join(inventory, "\n")))
	doc := M{"openapi": "3.1.0", "info": M{"title": "Pier HTTP API", "version": eval(&ast.SelectorExpr{X: &ast.Ident{Name: "version"}, Sel: &ast.Ident{Name: "API"}}, nil), "description": "Source-derived HTTP contract. Authorization intersects credential abilities with live instance/project membership. Unknown/global scopes require instance permission; roles do not grant access to unrelated projects. Schemas describe JSON representation, not every semantic validation. Extension x-schema-inference lists expressions whose exact shape cannot be inferred; these are deliberately not presented as typed SDK guarantees."}, "servers": []M{{"url": "/"}}, "paths": paths, "components": M{"schemas": schemas, "securitySchemes": M{"bearerAuth": M{"type": "http", "scheme": "bearer"}, "cookieAuth": M{"type": "apiKey", "in": "cookie", "name": "pier_session"}}}, "x-route-inventory-sha256": fmt.Sprintf("%x", sum), "x-route-count": len(routes)}
	data, err := json.MarshalIndent(doc, "", "  ")
	return append(data, '\n'), err
}
func reset() {
	fs = tokenSet()
	types = map[string]typ{}
	funcs = map[string]fun{}
	schemas = M{}
	routes = nil
	unresolved = nil
	globalVars = map[string]binding{}
	constantExpr = map[string]binding{}
	enumValues = map[string][]any{}
}

var placeholder = regexp.MustCompile(`\{([^}]+)\}`)

func operation(r route, method, path string) M {
	ability := map[string]string{"canRead": "read", "canDeploy": "deploy", "canWrite": "write", "sessionOnly": "write"}[r.Access]
	security := []M{{"bearerAuth": []string{}}, {"cookieAuth": []string{}}}
	if r.Access == "public" {
		security = []M{}
	}
	if r.Access == "sessionOnly" {
		security = []M{{"cookieAuth": []string{}}}
	}
	params := []M{}
	for _, m := range placeholder.FindAllStringSubmatch(path, -1) {
		params = append(params, M{"name": m[1], "in": "path", "required": true, "schema": M{"type": "string"}})
	}
	if r.Access != "public" && method != "get" {
		params = append(params, M{"name": "X-Pier-Client", "in": "header", "required": false, "description": "Required for cookie-authenticated mutations (CSRF protection); bearer-token requests do not require it.", "schema": M{"type": "string", "minLength": 1}})
	}
	op := M{"operationId": strings.NewReplacer(" ", "_", "/", "_", "{", "", "}", "", "-", "_").Replace(r.Pattern), "summary": r.Handler, "tags": []string{routeTag(path)}, "security": security, "x-pier-access": r.Access, "x-pier-ability": ability, "x-pier-session-only": r.Access == "sessionOnly", "x-source": r.Source, "parameters": params}
	responses := M{}
	inference := []string{}
	env := initialEnv()
	if r.params != nil {
		for _, f := range r.params.List {
			for _, n := range f.Names {
				env[n.Name] = binding{f.Type, r.s}
			}
		}
	}
	requestSchemas := []M{}
	requestRequired := false
	visited := map[string]bool{}
	var inspect func(ast.Node, scope, map[string]binding)
	inspect = func(body ast.Node, scopeNow scope, local map[string]binding) {
		oldScope := r.s
		r.s = scopeNow
		oldEnv := env
		env = local
		ast.Inspect(body, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.AssignStmt:
				bindAssignment(v, env, r.s)
			case *ast.DeclStmt:
				bindDeclaration(v, env, r.s)
			case *ast.SwitchStmt:
				if c, ok := v.Tag.(*ast.CallExpr); ok && len(c.Args) > 0 && strings.HasSuffix(source(c.Fun), ".PathValue") {
					name := eval(c.Args[0], nil)
					values := []any{}
					for _, entry := range v.Body.List {
						if cc, ok := entry.(*ast.CaseClause); ok {
							for _, value := range cc.List {
								if text := eval(value, nil); text != "" {
									values = append(values, text)
								}
							}
						}
					}
					for _, p := range op["parameters"].([]M) {
						if p["name"] == name && len(values) > 0 {
							p["schema"].(M)["enum"] = values
						}
					}
				}
			}
			// Follow only returned HTTP helper calls, never service implementation.
			if ret, yes := n.(*ast.ReturnStmt); yes {
				for _, value := range ret.Results {
					if call, yes := value.(*ast.CallExpr); yes {
						if fn, known := calledFunction(call.Fun, env, r.s); known && fn.s.pkg == "httpapi" && !isResponder(fn.decl.Name.Name) {
							id := source(fn.decl.Type) + fn.decl.Name.Name
							if !visited[id] {
								visited[id] = true
								inspect(fn.decl.Body, fn.s, initialEnv())
							}
						}
					}
				}
			}
			c, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if index, ok := c.Fun.(*ast.IndexExpr); ok && (source(index.X) == "decode" || source(index.X) == "decodeOptional") {
				requestSchemas = appendUnique(requestSchemas, schema(index.Index, r.s))
				if source(index.X) == "decode" {
					requestRequired = true
				}
			}
			if sel, ok := c.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Get" && strings.Contains(source(sel.X), ".Query()") && len(c.Args) > 0 {
				addQuery(op, source(c.Args[0]), "string")
			}
			name := source(c.Fun)
			if name == "queryFlag" && len(c.Args) > 1 {
				addQuery(op, source(c.Args[1]), "boolean")
			}
			args := c.Args
			code := map[string]string{"ok": "200", "created": "201", "accepted": "202", "noContent": "204"}[name]
			if name == "writeJSON" && len(c.Args) > 2 {
				code = map[string]string{"http.StatusOK": "200", "http.StatusCreated": "201", "http.StatusAccepted": "202"}[source(c.Args[1])]
				args = []ast.Expr{c.Args[0], c.Args[2]}
			}
			if code != "" {
				resp := M{"description": "Success"}
				if code != "204" && len(args) > 1 {
					t, ts, known := infer(args[1], env, r.s, 0)
					sc := M{}
					if known {
						sc = schema(t, ts)
					} else {
						inference = append(inference, "response "+source(args[1]))
						sc = M{"description": "Shape not statically inferred; see x-schema-inference.", "x-go-expression": source(args[1])}
					}
					resp["content"] = M{"application/json": M{"schema": sc}}
				}
				if old, ok := responses[code].(M); ok && old["content"] != nil && resp["content"] != nil {
					a := old["content"].(M)["application/json"].(M)["schema"].(M)
					b := resp["content"].(M)["application/json"].(M)["schema"].(M)
					list := []M{}
					if xs, ok := a["anyOf"].([]M); ok {
						list = xs
					} else {
						list = append(list, a)
					}
					resp["content"].(M)["application/json"].(M)["schema"] = union(appendUnique(list, b))
				}
				responses[code] = resp
			}
			return true
		})
		r.s = oldScope
		env = oldEnv
	}
	inspect(r.body, r.s, env)
	if len(requestSchemas) > 0 {
		op["requestBody"] = M{"required": requestRequired, "content": M{"application/json": M{"schema": union(requestSchemas)}}}
	}
	if len(responses) == 0 {
		responses["200"] = M{"description": "Handler-specific response. May be streamed, redirected, or binary; consult x-source and protocol notes."}
		inference = append(inference, "handler-specific success response")
	}
	specialResponses(op, responses, method, path)
	for _, code := range []string{"400", "401", "403", "404", "409", "413", "422", "429", "500", "502"} {
		responses[code] = M{"description": errorDescription(code), "content": M{"application/json": M{"schema": schema(&ast.Ident{Name: "errorBody"}, scope{pkg: "httpapi"})}}}
	}
	op["responses"] = responses
	if strings.Contains(source(r.body), "requireSensitive(") {
		op["x-pier-sensitive"] = true
		op["description"] = "Sensitive data: cookie session or write-capable token required in addition to scope permission."
	}
	if len(inference) > 0 {
		op["x-schema-inference"] = inference
	}
	return op
}
func appendUnique(list []M, v M) []M {
	b, _ := json.Marshal(v)
	for _, x := range list {
		a, _ := json.Marshal(x)
		if string(a) == string(b) {
			return list
		}
	}
	return append(list, v)
}
func union(list []M) M {
	if len(list) == 1 {
		return list[0]
	}
	return M{"anyOf": list}
}
func routeTag(path string) string {
	p := strings.Split(strings.TrimPrefix(path, "/api/v1/"), "/")
	return p[0]
}
func addQuery(op M, quoted, kind string) {
	name := strings.Trim(quoted, `"`)
	if name == quoted {
		return
	}
	ps := op["parameters"].([]M)
	for _, p := range ps {
		if p["name"] == name && p["in"] == "query" {
			return
		}
	}
	op["parameters"] = append(ps, M{"name": name, "in": "query", "schema": M{"type": kind}})
}
func errorDescription(code string) string {
	return map[string]string{"400": "Bad request / provider webhook rejected", "401": "Authentication required or expired", "403": "Credential, role, project scope, session-only or CSRF check refused", "404": "Resource not found", "409": "Conflicting operation or invalid lifecycle state", "413": "Body exceeds allowed size", "422": "Invalid fields or malformed JSON", "429": "Rate limit (Retry-After header)", "500": "Internal error (details omitted)", "502": "Dependency or server unavailable"}[code]
}

func isResponder(name string) bool {
	return name == "ok" || name == "created" || name == "accepted" || name == "noContent" || name == "writeJSON"
}
