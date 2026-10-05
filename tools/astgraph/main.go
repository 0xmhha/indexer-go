// astgraph builds a typed code graph of a Go module.
//
// Nodes: packages, functions, methods, types, interface methods.
// Edges: import, ext_import, call, iface_call, ref, implements, contains.
// It also records concurrency/IO signals per declaration (go stmts, channel
// ops, selects, mutex use, time.Sleep) and concrete type assertions.
//
// usage: astgraph -dir <module root> -out <dir>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Node struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"` // package|func|method|type|interface|iface_method
	Pkg      string `json:"pkg"`
	Name     string `json:"name"`
	File     string `json:"file,omitempty"`
	Line     int    `json:"line,omitempty"`
	Lines    int    `json:"lines,omitempty"`
	Exported bool   `json:"exported,omitempty"`
	// signals
	GoStmts  int `json:"go_stmts,omitempty"`
	Selects  int `json:"selects,omitempty"`
	ChanOps  int `json:"chan_ops,omitempty"`
	MakeChan int `json:"make_chan,omitempty"`
	Locks    int `json:"locks,omitempty"`
	Sleeps   int `json:"sleeps,omitempty"`
	Atomics  int `json:"atomics,omitempty"`
	Sprintf  int `json:"sprintf,omitempty"`
	JSON     int `json:"json,omitempty"`
	Methods  int `json:"methods,omitempty"` // method set size for types / method count for interfaces
}

type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	N    int    `json:"n,omitempty"`
}

type Assertion struct {
	At     string `json:"at"`
	From   string `json:"from"`
	Target string `json:"target"`
}

type Graph struct {
	Module     string      `json:"module"`
	Nodes      []*Node     `json:"nodes"`
	Edges      []*Edge     `json:"edges"`
	Assertions []Assertion `json:"concrete_assertions"`
}

var (
	modPath string
	root    string
	nodes   = map[string]*Node{}
	edges   = map[[3]string]*Edge{}
	asserts []Assertion
	fset    *token.FileSet
)

func internal(p *types.Package) bool {
	return p != nil && (p.Path() == modPath || strings.HasPrefix(p.Path(), modPath+"/"))
}

func addEdge(from, to, kind string) {
	if from == "" || to == "" || from == to {
		return
	}
	k := [3]string{from, to, kind}
	if e, ok := edges[k]; ok {
		e.N++
		return
	}
	edges[k] = &Edge{From: from, To: to, Kind: kind, N: 1}
}

func recvName(t types.Type) (string, bool) {
	if p, ok := t.(*types.Pointer); ok {
		t = p.Elem()
	}
	switch n := t.(type) {
	case *types.Named:
		_, isIface := n.Underlying().(*types.Interface)
		return n.Obj().Name(), isIface
	case *types.Alias:
		return recvName(types.Unalias(n))
	}
	return "", false
}

func funcID(f *types.Func) (string, bool) {
	f = f.Origin()
	if f.Pkg() == nil {
		return "", false
	}
	sig := f.Type().(*types.Signature)
	if r := sig.Recv(); r != nil {
		name, iface := recvName(r.Type())
		if name == "" {
			return "", false
		}
		if iface {
			return f.Pkg().Path() + "." + name + "." + f.Name(), true
		}
		return f.Pkg().Path() + ".(" + name + ")." + f.Name(), false
	}
	return f.Pkg().Path() + "." + f.Name(), false
}

func rel(pos token.Pos) (string, int) {
	p := fset.Position(pos)
	r, err := filepath.Rel(root, p.Filename)
	if err != nil {
		r = p.Filename
	}
	return r, p.Line
}

func lines(n ast.Node) int {
	return fset.Position(n.End()).Line - fset.Position(n.Pos()).Line + 1
}

func main() {
	dir := flag.String("dir", ".", "module root")
	out := flag.String("out", "out", "output dir")
	flag.Parse()
	root, _ = filepath.Abs(*dir)

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax | packages.NeedTypes |
			packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps | packages.NeedModule,
		Dir:   root,
		Tests: false,
		Env:   append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=readonly"),
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		log.Fatal(err)
	}
	if packages.PrintErrors(pkgs) > 0 {
		log.Println("warning: load errors above")
	}
	fset = pkgs[0].Fset
	modPath = pkgs[0].Module.Path

	var ifaces []*types.Named
	var concretes []*types.Named

	// pass 1: declarations
	for _, p := range pkgs {
		pn := &Node{ID: p.PkgPath, Kind: "package", Pkg: p.PkgPath, Name: p.Name}
		for _, f := range p.Syntax {
			pn.Lines += lines(f)
		}
		nodes[p.PkgPath] = pn
		for path, ip := range p.Imports {
			if internal(ip.Types) {
				addEdge(p.PkgPath, path, "import")
			} else if ip.Module != nil {
				addEdge(p.PkgPath, "mod:"+ip.Module.Path, "ext_import")
			}
		}
		scope := p.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok {
				continue
			}
			named, ok := tn.Type().(*types.Named)
			if !ok {
				continue
			}
			if it, ok := named.Underlying().(*types.Interface); ok {
				if it.NumMethods() > 0 {
					ifaces = append(ifaces, named)
				}
				id := p.PkgPath + "." + name
				n := ensure(id, "interface", p.PkgPath, name)
				n.Methods = it.NumMethods()
				for i := 0; i < it.NumMethods(); i++ {
					m := it.Method(i)
					mid := id + "." + m.Name()
					mn := ensure(mid, "iface_method", p.PkgPath, name+"."+m.Name())
					f, l := rel(m.Pos())
					mn.File, mn.Line = f, l
					addEdge(id, mid, "contains")
				}
			} else {
				concretes = append(concretes, named)
				ms := types.NewMethodSet(types.NewPointer(named))
				n := ensure(p.PkgPath+"."+name, "type", p.PkgPath, name)
				n.Methods = ms.Len()
			}
		}
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					obj, _ := p.TypesInfo.Defs[d.Name].(*types.Func)
					if obj == nil {
						continue
					}
					id, _ := funcID(obj)
					kind := "func"
					if d.Recv != nil {
						kind = "method"
					}
					n := ensure(id, kind, p.PkgPath, strings.TrimPrefix(id, p.PkgPath+"."))
					n.File, n.Line = rel(d.Pos())
					n.Lines = lines(d)
					n.Exported = d.Name.IsExported()
					addEdge(p.PkgPath, id, "contains")
				case *ast.GenDecl:
					for _, s := range d.Specs {
						if ts, ok := s.(*ast.TypeSpec); ok {
							id := p.PkgPath + "." + ts.Name.Name
							kind := "type"
							if _, ok := ts.Type.(*ast.InterfaceType); ok {
								kind = "interface"
							}
							n := ensure(id, kind, p.PkgPath, ts.Name.Name)
							n.File, n.Line = rel(ts.Pos())
							n.Lines = lines(ts)
							n.Exported = ts.Name.IsExported()
							addEdge(p.PkgPath, id, "contains")
						}
					}
				}
			}
		}
	}

	// pass 2: bodies
	for _, p := range pkgs {
		for _, f := range p.Syntax {
			for _, d := range f.Decls {
				owner := p.PkgPath
				switch d := d.(type) {
				case *ast.FuncDecl:
					if obj, _ := p.TypesInfo.Defs[d.Name].(*types.Func); obj != nil {
						owner, _ = funcID(obj)
					}
				case *ast.GenDecl:
					if len(d.Specs) == 1 {
						if ts, ok := d.Specs[0].(*ast.TypeSpec); ok {
							owner = p.PkgPath + "." + ts.Name.Name
						}
					}
				}
				walk(p, owner, d)
			}
		}
	}

	// implements
	for _, c := range concretes {
		if !c.Obj().Exported() && false {
			continue
		}
		ptr := types.NewPointer(c)
		for _, i := range ifaces {
			it := i.Underlying().(*types.Interface)
			if types.Implements(c, it) || types.Implements(ptr, it) {
				addEdge(c.Obj().Pkg().Path()+"."+c.Obj().Name(), i.Obj().Pkg().Path()+"."+i.Obj().Name(), "implements")
			}
		}
	}

	g := Graph{Module: modPath, Assertions: asserts}
	for _, n := range nodes {
		g.Nodes = append(g.Nodes, n)
	}
	for _, e := range edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Nodes, func(i, j int) bool { return g.Nodes[i].ID < g.Nodes[j].ID })
	sort.Slice(g.Edges, func(i, j int) bool {
		a, b := g.Edges[i], g.Edges[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	os.MkdirAll(*out, 0o755)
	fh, err := os.Create(filepath.Join(*out, "graph.json"))
	if err != nil {
		log.Fatal(err)
	}
	enc := json.NewEncoder(fh)
	enc.SetIndent("", " ")
	if err := enc.Encode(g); err != nil {
		log.Fatal(err)
	}
	fh.Close()
	fmt.Printf("module=%s packages=%d nodes=%d edges=%d\n", modPath, len(pkgs), len(g.Nodes), len(g.Edges))
}

func ensure(id, kind, pkg, name string) *Node {
	if n, ok := nodes[id]; ok {
		return n
	}
	n := &Node{ID: id, Kind: kind, Pkg: pkg, Name: name}
	nodes[id] = n
	return n
}

func isPkgFunc(p *packages.Package, call *ast.CallExpr, pkgPath, name string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != name {
		return false
	}
	obj, ok := p.TypesInfo.Uses[sel.Sel].(*types.Func)
	return ok && obj.Pkg() != nil && obj.Pkg().Path() == pkgPath
}

func walk(p *packages.Package, owner string, root ast.Node) {
	n := nodes[owner]
	ast.Inspect(root, func(x ast.Node) bool {
		switch x := x.(type) {
		case *ast.GoStmt:
			if n != nil {
				n.GoStmts++
			}
		case *ast.SelectStmt:
			if n != nil {
				n.Selects++
			}
		case *ast.SendStmt:
			if n != nil {
				n.ChanOps++
			}
		case *ast.UnaryExpr:
			if x.Op == token.ARROW && n != nil {
				n.ChanOps++
			}
		case *ast.TypeAssertExpr:
			if x.Type == nil {
				break
			}
			t := p.TypesInfo.TypeOf(x.Type)
			if t == nil {
				break
			}
			if _, isIface := t.Underlying().(*types.Interface); isIface {
				break
			}
			base := t
			if pt, ok := t.(*types.Pointer); ok {
				base = pt.Elem()
			}
			if nm, ok := base.(*types.Named); ok && internal(nm.Obj().Pkg()) {
				at, line := rel(x.Pos())
				asserts = append(asserts, Assertion{At: fmt.Sprintf("%s:%d", at, line), From: owner, Target: types.TypeString(t, nil)})
			}
		case *ast.CallExpr:
			if n != nil {
				switch {
				case isPkgFunc(p, x, "time", "Sleep"):
					n.Sleeps++
				case isPkgFunc(p, x, "fmt", "Sprintf"):
					n.Sprintf++
				case isPkgFunc(p, x, "encoding/json", "Marshal"), isPkgFunc(p, x, "encoding/json", "Unmarshal"):
					n.JSON++
				}
				if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "make" && len(x.Args) > 0 {
					if t := p.TypesInfo.TypeOf(x.Args[0]); t != nil {
						if _, ok := t.Underlying().(*types.Chan); ok {
							n.MakeChan++
						}
					}
				}
				if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
					if obj, ok := p.TypesInfo.Uses[sel.Sel].(*types.Func); ok && obj.Pkg() != nil {
						switch obj.Pkg().Path() {
						case "sync":
							if obj.Name() == "Lock" || obj.Name() == "RLock" {
								n.Locks++
							}
						case "sync/atomic":
							n.Atomics++
						}
						if sig, ok := obj.Type().(*types.Signature); ok && sig.Recv() != nil {
							if nm, ok := sig.Recv().Type().(*types.Pointer); ok {
								if named, ok := nm.Elem().(*types.Named); ok && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "sync/atomic" {
									n.Atomics++
								}
							}
						}
					}
				}
			}
			var fobj *types.Func
			switch fn := x.Fun.(type) {
			case *ast.Ident:
				fobj, _ = p.TypesInfo.Uses[fn].(*types.Func)
			case *ast.SelectorExpr:
				fobj, _ = p.TypesInfo.Uses[fn.Sel].(*types.Func)
			case *ast.IndexExpr:
				if id, ok := fn.X.(*ast.Ident); ok {
					fobj, _ = p.TypesInfo.Uses[id].(*types.Func)
				} else if s, ok := fn.X.(*ast.SelectorExpr); ok {
					fobj, _ = p.TypesInfo.Uses[s.Sel].(*types.Func)
				}
			}
			if fobj != nil && internal(fobj.Pkg()) {
				if id, iface := funcID(fobj); id != "" {
					if iface {
						addEdge(owner, id, "iface_call")
					} else {
						addEdge(owner, id, "call")
					}
				}
			}
		case *ast.Ident:
			switch o := p.TypesInfo.Uses[x].(type) {
			case *types.TypeName:
				if internal(o.Pkg()) && o.Parent() == o.Pkg().Scope() {
					addEdge(owner, o.Pkg().Path()+"."+o.Name(), "ref")
				}
			case *types.Func:
				// function value references (not calls) are captured as calls above when called;
				// keep a ref edge so method values/callbacks are not lost.
				if internal(o.Pkg()) {
					if id, _ := funcID(o); id != "" {
						addEdge(owner, id, "ref")
					}
				}
			}
		}
		return true
	})
}
