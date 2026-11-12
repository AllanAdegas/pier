// Command openapi maintains a local, source-derived OpenAPI inventory.
package main

import (
 "bytes"
 "crypto/sha256"
 "encoding/json"
 "flag"
 "fmt"
 "go/ast"
 "go/format"
 "go/parser"
 "go/token"
 "os"
 "path/filepath"
 "reflect"
 "sort"
 "strconv"
 "strings"
)
type M = map[string]any
type scope struct { pkg string; imports map[string]string }
type typ struct { expr ast.Expr; s scope }
type fun struct { decl *ast.FuncDecl; s scope }
type route struct { Pattern, Access, Handler, Source string; body ast.Node; s scope }
var fs = token.NewFileSet()
var types = map[string]typ{}
var funcs = map[string]fun{}
var schemas = M{}
var routes []route
var unresolved []string
func source(n ast.Node) string { var b bytes.Buffer; if n != nil { _ = format.Node(&b,fs,n) }; return b.String() }
func key(s scope, name string) string { return s.pkg+"."+name }
func refName(e ast.Expr,s scope) string {
 switch n:=e.(type) {case *ast.Ident: return key(s,n.Name); case *ast.SelectorExpr: if p,ok:=n.X.(*ast.Ident); ok {return s.imports[p.Name]+"."+n.Sel.Name}; case *ast.StarExpr: return refName(n.X,s)}; return ""
}
func schema(e ast.Expr,s scope) M {
 switch n:=e.(type) {
 case *ast.StarExpr: return M{"anyOf":[]any{schema(n.X,s),M{"type":"null"}}}
 case *ast.ArrayType: return M{"type":"array","items":schema(n.Elt,s)}
 case *ast.MapType: return M{"type":"object","additionalProperties":schema(n.Value,s)}
 case *ast.InterfaceType: return M{"type":[]string{"object","array","string","number","boolean","null"},"description":"Arbitrary JSON supplied by the underlying engine/provider."}
 case *ast.StructType:
  props:=M{}; required:=[]string{}
  for _,f:=range n.Fields.List {
   if len(f.Names)==0 { embedded:=schema(f.Type,s); if r,ok:=embedded["$ref"].(string);ok {embedded=schemas[strings.TrimPrefix(r,"#/components/schemas/")].(M)}; if p,ok:=embedded["properties"].(M);ok {for k,v:=range p {props[k]=v}};continue }
   tag:=""; if f.Tag!=nil {v,_:=strconv.Unquote(f.Tag.Value); tag=reflect.StructTag(v).Get("json")}; if tag=="-" {continue}; parts:=strings.Split(tag,","); name:=parts[0];if name=="" {if !f.Names[0].IsExported(){continue}; name=f.Names[0].Name}
   props[name]=schema(f.Type,s); if !strings.Contains(tag,"omitempty") {required=append(required,name)}
  }; out:=M{"type":"object","properties":props};if len(required)>0 {out["required"]=required};return out
 case *ast.Ident:
  switch n.Name {case "string":return M{"type":"string"};case "bool":return M{"type":"boolean"};case "float32","float64":return M{"type":"number"};case "int","int8","int16","int32","int64","uint","uint8","uint16","uint32","uint64","byte":return M{"type":"integer"};case "any":return schema(&ast.InterfaceType{},s)}
 }
 name:=refName(e,s); if name=="time.Time" {return M{"type":"string","format":"date-time"}};if name=="json.RawMessage" {return schema(&ast.InterfaceType{},s)};if name=="time.Duration" {return M{"type":"integer","description":"Duration in nanoseconds."}}
 if t,ok:=types[name];ok { id:=strings.ReplaceAll(name,"/","_"); if _,exists:=schemas[id];!exists {schemas[id]=M{"type":"object"};schemas[id]=schema(t.expr,t.s)};return M{"$ref":"#/components/schemas/"+id} }
 return M{"type":"string","description":"Go scalar: "+source(e)}
}
func parseRepo() {
 _=filepath.WalkDir("internal",func(path string,d os.DirEntry,err error) error {
 if err!=nil{return err};if d.IsDir()||!strings.HasSuffix(path,".go")||strings.HasSuffix(path,"_test.go"){return nil}
 f,err:=parser.ParseFile(fs,path,nil,0);if err!=nil {panic(err)};s:=scope{pkg:strings.TrimPrefix(filepath.Dir(path),"internal/"),imports:map[string]string{}}
 // Package names, rather than directories, match imported selectors.
 s.pkg=f.Name.Name
 for _,i:=range f.Imports {v,_:=strconv.Unquote(i.Path.Value);n:=filepath.Base(v);if i.Name!=nil {n=i.Name.Name}; s.imports[n]=n; if strings.HasPrefix(v,"pier/internal/"){s.imports[n]=filepath.Base(v)} }
 for _,d:=range f.Decls {switch n:=d.(type) {case *ast.GenDecl:for _,sp:=range n.Specs {if t,ok:=sp.(*ast.TypeSpec);ok {types[key(s,t.Name.Name)]=typ{t.Type,s}}};case *ast.FuncDecl:
 name:=key(s,n.Name.Name);if n.Recv!=nil {name=refName(n.Recv.List[0].Type,s)+"."+n.Name.Name};funcs[name]=fun{n,s}
 }};return nil
 });
}
func eval(e ast.Expr,env map[string]string) string {switch n:=e.(type){case *ast.BasicLit:v,_:=strconv.Unquote(n.Value);return v;case *ast.Ident:return env[n.Name];case *ast.BinaryExpr:l,r:=eval(n.X,env),eval(n.Y,env);if l!=""&&r!=""{return l+r}};return ""}
func scanRoutes() {
 for _,f:=range funcs {if f.s.pkg!="httpapi" {continue};env:=map[string]string{"terminalRoute":"GET /api/v1/terminal"}
 ast.Inspect(f.decl.Body,func(n ast.Node)bool {if a,ok:=n.(*ast.AssignStmt);ok&&len(a.Lhs)==1&&len(a.Rhs)==1 {if id,ok:=a.Lhs[0].(*ast.Ident);ok {env[id.Name]=eval(a.Rhs[0],env)}}; c,ok:=n.(*ast.CallExpr);if !ok{return true};sel,ok:=c.Fun.(*ast.SelectorExpr);if !ok{return true};name:=sel.Sel.Name
 if (name!="route"&&name!="public"&&name!="HandleFunc")||len(c.Args)<2{return true};pat:=eval(c.Args[1],env);idx:=2;acc:="public";if name=="HandleFunc" {pat=eval(c.Args[0],env);idx=1};if name=="route" {acc=source(c.Args[2]);idx=3}
 pats:=[]string{pat};if pat=="" {expr:=source(c.Args[1]);switch f.decl.Name.Name {case "registerEnvRoutes":pats=nil;for _,coll:=range []string{"applications","databases","stacks"}{e:=map[string]string{"base":"/api/v1/"+coll+"/{id}/envs"};pats=append(pats,eval(c.Args[1],e))};case "registerVolumeBackupRoutes":pats=nil;for _,coll:=range []string{"applications","stacks"}{e:=map[string]string{"base":"/api/v1/"+coll+"/{id}"};pats=append(pats,eval(c.Args[1],e))};default:unresolved=append(unresolved,f.decl.Name.Name+": "+expr);return true}}
 for _,p:=range pats {if !strings.Contains(p," "){continue};h:=c.Args[idx];body:=ast.Node(h);hn:=source(h);if se,ok:=h.(*ast.SelectorExpr);ok {if fn,yes:=funcs["httpapi.API."+se.Sel.Name];yes {body=fn.decl.Body}};if call,ok:=h.(*ast.CallExpr);ok {if se,yes:=call.Fun.(*ast.SelectorExpr);yes {if se.Sel.Name=="volumeResource" {se,_=call.Args[1].(*ast.SelectorExpr)};if se!=nil {if fn,yes:=funcs["httpapi.API."+se.Sel.Name];yes {body=fn.decl.Body}}}}
 routes=append(routes,route{p,acc,hn,fs.Position(c.Pos()).Filename,body,f.s})};return true})
 };sort.Slice(routes,func(i,j int)bool{return routes[i].Pattern<routes[j].Pattern})
}
