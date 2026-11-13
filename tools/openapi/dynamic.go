package main

import (
	"go/ast"
	"sort"
)

// Dynamic registrations are expanded from their actual call sites/range values.
// Unknown expressions remain an error rather than dropping an HTTP endpoint.
func dynamicPatterns(f fun, e ast.Expr) []string {
	var base ast.Expr
	ast.Inspect(f.decl.Body, func(n ast.Node) bool {
		if a, ok := n.(*ast.AssignStmt); ok && len(a.Lhs) == 1 && len(a.Rhs) == 1 && source(a.Lhs[0]) == "base" {
			base = a.Rhs[0]
		}
		return true
	})
	if base == nil {
		return nil
	}
	values := map[string]bool{}
	for _, caller := range funcs {
		ast.Inspect(caller.decl.Body, func(n ast.Node) bool {
			if c, ok := n.(*ast.CallExpr); ok {
				if sel, yes := c.Fun.(*ast.SelectorExpr); yes && sel.Sel.Name == f.decl.Name.Name && len(c.Args) > 1 {
					if v := eval(c.Args[1], nil); v != "" {
						values[v] = true
					}
				}
			}
			return true
		})
	}
	variable := "collection"
	if len(values) == 0 {
		ast.Inspect(f.decl.Body, func(n ast.Node) bool {
			r, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			var lit *ast.CompositeLit
			if v, ok := r.X.(*ast.CompositeLit); ok {
				lit = v
				variable = source(r.Value)
			}
			if id, ok := r.X.(*ast.Ident); ok {
				if b, yes := constantExpr["httpapi."+id.Name]; yes {
					lit, _ = b.e.(*ast.CompositeLit)
					variable = source(r.Key)
				}
			}
			if lit != nil {
				for _, elt := range lit.Elts {
					if pair, ok := elt.(*ast.KeyValueExpr); ok {
						elt = pair.Key
					}
					v := eval(elt, nil)
					if v != "" {
						values[v] = true
					}
				}
			}
			return true
		})
	}
	result := []string{}
	for value := range values {
		b := eval(base, map[string]string{variable: value})
		if b != "" {
			result = append(result, eval(e, map[string]string{"base": b}))
		}
	}
	sort.Strings(result)
	return result
}
