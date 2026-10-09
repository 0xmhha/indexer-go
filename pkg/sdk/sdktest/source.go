package sdktest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// AllowComment, on the line of a call or the line above, accepts it: code
// that runs outside block handlers (an HTTP route's cache, a background
// job) may read the clock. Give the reason after it.
const AllowComment = "//sdk:nondeterministic"

// forbidden lists, per import path, the functions and variables a handler
// must not use; "*" is every one of the package.
var forbidden = map[string]map[string]string{
	"time": {
		"Now": "reads the clock", "Since": "reads the clock", "Until": "reads the clock",
		"After": "uses the clock", "AfterFunc": "uses the clock", "Tick": "uses the clock",
		"NewTicker": "uses the clock", "NewTimer": "uses the clock",
	},
	"math/rand":    {"*": "is random"},
	"math/rand/v2": {"*": "is random"},
	"crypto/rand":  {"*": "is random"},
	"os": {
		"Getenv": "reads the environment", "LookupEnv": "reads the environment", "Environ": "reads the environment",
		"Hostname": "reads the host", "ReadFile": "reads files", "Open": "reads files",
	},
	"net/http": {
		"Get": "calls the network", "Head": "calls the network", "Post": "calls the network", "PostForm": "calls the network",
		"NewRequest": "calls the network", "NewRequestWithContext": "calls the network", "DefaultClient": "calls the network",
	},
	"net": {"*": "calls the network"},
}

// nodeReads are node calls whose last argument is the block to read at; nil
// reads the latest state, which differs between live indexing and backfill.
var nodeReads = map[string]bool{"CallContract": true, "CodeAt": true, "BalanceAt": true, "StorageAt": true, "NonceAt": true}

// Finding is a use of something the determinism rules forbid.
type Finding struct {
	Pos    token.Position
	Use    string
	Reason string
}

func (f Finding) String() string {
	return fmt.Sprintf("%s:%d: %s %s", f.Pos.Filename, f.Pos.Line, f.Use, f.Reason)
}

// ScanSource returns the uses the determinism rules forbid in the non-test
// Go files of the package in dir (not its subdirectories): the clock,
// randomness, the environment, files and the network, and node reads of
// the latest state (CallContract, CodeAt, BalanceAt, StorageAt, NonceAt
// with a nil block). A use with AllowComment on its line or the line above
// is accepted. It reads syntax only: calls into other packages are not
// followed, and a local variable named like an imported package is taken
// for the package.
func ScanSource(dir string) ([]Finding, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	fset := token.NewFileSet()
	var out []Finding
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			return nil, err
		}
		f, err := parser.ParseFile(fset, name, src, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		out = append(out, scanFile(fset, f)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Pos.Filename != out[j].Pos.Filename {
			return out[i].Pos.Filename < out[j].Pos.Filename
		}
		return out[i].Pos.Line < out[j].Pos.Line
	})
	return out, nil
}

func scanFile(fset *token.FileSet, f *ast.File) []Finding {
	imports := map[string]string{} // local name -> path
	for _, im := range f.Imports {
		path, _ := strconv.Unquote(im.Path.Value)
		name := path[strings.LastIndex(path, "/")+1:]
		if path == "math/rand/v2" {
			name = "rand"
		}
		if im.Name != nil {
			name = im.Name.Name
		}
		imports[name] = path
	}
	allowed := map[int]bool{}
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, AllowComment) {
				line := fset.Position(c.Pos()).Line
				allowed[line], allowed[line+1] = true, true
			}
		}
	}

	var out []Finding
	add := func(n ast.Node, use, reason string) {
		pos := fset.Position(n.Pos())
		if !allowed[pos.Line] {
			out = append(out, Finding{Pos: pos, Use: use, Reason: reason})
		}
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.SelectorExpr:
			id, ok := n.X.(*ast.Ident)
			if !ok {
				return true
			}
			path, ok := imports[id.Name]
			if !ok {
				return true
			}
			names := forbidden[path]
			if reason, ok := names[n.Sel.Name]; ok {
				add(n, path+"."+n.Sel.Name, reason)
			} else if reason, ok := names["*"]; ok {
				add(n, path+"."+n.Sel.Name, reason)
			}
		case *ast.CallExpr:
			sel, ok := n.Fun.(*ast.SelectorExpr)
			if !ok || !nodeReads[sel.Sel.Name] || len(n.Args) == 0 {
				return true
			}
			if last, ok := n.Args[len(n.Args)-1].(*ast.Ident); ok && last.Name == "nil" {
				add(n, sel.Sel.Name+"(..., nil)", "reads the node's latest state; pass the block's number")
			}
		}
		return true
	})
	return out
}

// RequireNoForbiddenUses fails with every use ScanSource finds in the
// packages in dirs.
func RequireNoForbiddenUses(t testing.TB, dirs ...string) {
	t.Helper()
	var all []string
	for _, dir := range dirs {
		found, err := ScanSource(dir)
		if err != nil {
			t.Fatalf("sdktest: scan %s: %v", dir, err)
		}
		for _, f := range found {
			all = append(all, f.String())
		}
	}
	if len(all) > 0 {
		t.Fatalf("sdktest: handler code uses what the determinism rules forbid (accept a use outside handlers with %s <reason>):\n  %s",
			AllowComment, strings.Join(all, "\n  "))
	}
}
