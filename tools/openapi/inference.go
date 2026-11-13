package main

import (
	"go/ast"
	"go/token"
	"strings"
)

type binding struct {
	e ast.Expr
	s scope
}

func tokenSet() *token.FileSet { return token.NewFileSet() }
func initialEnv() map[string]binding {
	return map[string]binding{"a": {&ast.Ident{Name: "API"}, scope{pkg: "httpapi"}}}
}
func bindAssignment(a *ast.AssignStmt, env map[string]binding, s scope) {
	for i, l := range a.Lhs {
		ident, ok := l.(*ast.Ident)
		if !ok || ident.Name == "_" {
			continue
		}
		var e ast.Expr
		if i < len(a.Rhs) {
			e = a.Rhs[i]
		} else if len(a.Rhs) == 1 {
			e = a.Rhs[0]
		}
		if e == nil {
			continue
		}
		t, ts, ok := infer(e, env, s, i)
		if ok {
			env[ident.Name] = binding{t, ts}
		}
	}
}
func bindDeclaration(d *ast.DeclStmt, env map[string]binding, s scope) {
	g, ok := d.Decl.(*ast.GenDecl)
	if !ok {
		return
	}
	for _, sp := range g.Specs {
		v, ok := sp.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range v.Names {
			if v.Type != nil {
				env[n.Name] = binding{v.Type, s}
			}
		}
	}
}
func infer(e ast.Expr, env map[string]binding, s scope, result int) (ast.Expr, scope, bool) {
	if e == nil {
		return nil, s, false
	}
	switch n := e.(type) {
	case *ast.Ident:
		if b, ok := env[n.Name]; ok {
			return b.e, b.s, true
		}
		if n.Name == "true" || n.Name == "false" {
			return &ast.Ident{Name: "bool"}, s, true
		}
	case *ast.UnaryExpr:
		return infer(n.X, env, s, result)
	case *ast.CompositeLit:
		if n.Type != nil {
			return n.Type, s, true
		}
	case *ast.BasicLit:
		if n.Kind == token.STRING {
			return &ast.Ident{Name: "string"}, s, true
		}
		return &ast.Ident{Name: "int"}, s, true
	case *ast.SliceExpr:
		return infer(n.X, env, s, 0)
	case *ast.CallExpr:
		if source(n.Fun) == "make" && len(n.Args) > 0 {
			return n.Args[0], s, true
		}
		if source(n.Fun) == "append" && len(n.Args) > 0 {
			return infer(n.Args[0], env, s, 0)
		}
		if x, ok := n.Fun.(*ast.IndexExpr); ok && (source(x.X) == "decode" || source(x.X) == "decodeOptional") {
			return x.Index, s, true
		}
		fn := source(n.Fun)
		if (fn == "forCaller" || fn == "allForCaller") && len(n.Args) > 1 {
			return infer(n.Args[1], env, s, 0)
		}
		f, ok := calledFunction(n.Fun, env, s)
		if ok && f.decl.Type.Results != nil {
			index := 0
			for _, field := range f.decl.Type.Results.List {
				count := len(field.Names)
				if count == 0 {
					count = 1
				}
				if result < index+count {
					return field.Type, f.s, true
				}
				index += count
			}
		}
	case *ast.SelectorExpr:
		if id, ok := n.X.(*ast.Ident); ok {
			if b, yes := globalVars[s.imports[id.Name]+"."+n.Sel.Name]; yes {
				return b.e, b.s, true
			}
		}
		t, ts, ok := infer(n.X, env, s, 0)
		if ok {
			if field, yes := fieldType(t, ts, n.Sel.Name, 0); yes {
				return field.e, field.s, true
			}
		}
	case *ast.IndexExpr:
		t, ts, ok := infer(n.X, env, s, 0)
		if ok {
			if ar, yes := t.(*ast.ArrayType); yes {
				return ar.Elt, ts, true
			}
		}
	}
	return nil, s, false
}
func fieldType(e ast.Expr, s scope, name string, depth int) (binding, bool) {
	if depth > 8 {
		return binding{}, false
	}
	if p, ok := e.(*ast.StarExpr); ok {
		return fieldType(p.X, s, name, depth+1)
	}
	if st, ok := e.(*ast.StructType); ok {
		for _, f := range st.Fields.List {
			for _, n := range f.Names {
				if n.Name == name {
					return binding{f.Type, s}, true
				}
			}
			if len(f.Names) == 0 {
				if b, yes := fieldType(f.Type, s, name, depth+1); yes {
					return b, true
				}
			}
		}
		return binding{}, false
	}
	if t, ok := types[refName(e, s)]; ok {
		return fieldType(t.expr, t.s, name, depth+1)
	}
	return binding{}, false
}
func calledFunction(e ast.Expr, env map[string]binding, s scope) (fun, bool) {
	if call, ok := e.(*ast.CallExpr); ok {
		return calledFunction(call.Fun, env, s)
	}
	if id, ok := e.(*ast.Ident); ok {
		f, yes := funcs[key(s, id.Name)]
		return f, yes
	}
	if sel, ok := e.(*ast.SelectorExpr); ok {
		if id, yes := sel.X.(*ast.Ident); yes {
			if pkg, exists := s.imports[id.Name]; exists {
				f, found := funcs[pkg+"."+sel.Sel.Name]
				return f, found
			}
		}
		t, ts, yes := infer(sel.X, env, s, 0)
		if yes {
			name := strings.TrimPrefix(refName(t, ts), "*")
			f, found := funcs[name+"."+sel.Sel.Name]
			return f, found
		}
	}
	return fun{}, false
}
