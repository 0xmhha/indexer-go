package sdk_test

import (
	"flag"
	"fmt"
	"go/types"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

var update = flag.Bool("update", false, "rewrite testdata/surface.txt")

// TestSDKSurface pins what projects build on (docs/SDK.md "호환성 약속"):
// every exported identifier of pkg/sdk and pkg/sdk/sdktest, with the
// methods and fields of the types they alias, so any change to the SDK
// is seen and reviewed. Removing or changing an entry is a breaking change;
// adding one is not, except a method of an interface projects implement.
// After reviewing, rewrite it with go test ./pkg/sdk -run TestSDKSurface -update.
func TestSDKSurface(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedTypes},
		"github.com/0xmhha/indexer-go/pkg/sdk", "github.com/0xmhha/indexer-go/pkg/sdk/sdktest")
	require.NoError(t, err)
	var b strings.Builder
	for _, p := range pkgs {
		require.Empty(t, p.Errors)
		fmt.Fprintf(&b, "package %s\n", p.PkgPath)
		describeScope(&b, p.Types.Scope())
		b.WriteString("\n")
	}
	got := b.String()
	if *update {
		require.NoError(t, os.WriteFile("testdata/surface.txt", []byte(got), 0o644))
	}
	want, err := os.ReadFile("testdata/surface.txt")
	require.NoError(t, err)
	require.Equal(t, string(want), got, "the SDK surface changed; removing or changing an entry breaks projects "+
		"(docs/SDK.md). Review, note it in docs/SDK.md, then run go test ./pkg/sdk -run TestSDKSurface -update")
}

func qualifier(p *types.Package) string { return p.Name() }

func describeScope(b *strings.Builder, s *types.Scope) {
	for _, name := range s.Names() {
		obj := s.Lookup(name)
		if !obj.Exported() {
			continue
		}
		switch o := obj.(type) {
		case *types.TypeName:
			if o.IsAlias() {
				fmt.Fprintf(b, "type %s = %s\n", name, types.TypeString(types.Unalias(o.Type()), qualifier))
			} else {
				fmt.Fprintf(b, "type %s %s\n", name, kind(o.Type()))
			}
			describeType(b, o.Type())
		case *types.Func:
			fmt.Fprintf(b, "func %s%s\n", name, strings.TrimPrefix(types.TypeString(o.Type(), qualifier), "func"))
		case *types.Var:
			fmt.Fprintf(b, "var %s %s\n", name, types.TypeString(o.Type(), qualifier))
		case *types.Const:
			fmt.Fprintf(b, "const %s %s\n", name, types.TypeString(o.Type(), qualifier))
		}
	}
}

func kind(t types.Type) string {
	switch u := t.Underlying().(type) {
	case *types.Interface:
		return "interface"
	case *types.Struct:
		return "struct"
	default:
		return types.TypeString(u, qualifier)
	}
}

// describeType lists the exported methods and fields of a type.
func describeType(b *strings.Builder, t types.Type) {
	var lines []string
	if st, ok := t.Underlying().(*types.Struct); ok {
		for i := range st.NumFields() {
			if f := st.Field(i); f.Exported() {
				lines = append(lines, fmt.Sprintf("  field %s %s", f.Name(), types.TypeString(f.Type(), qualifier)))
			}
		}
	}
	var ms *types.MethodSet
	if _, ok := t.Underlying().(*types.Interface); ok {
		ms = types.NewMethodSet(t)
	} else {
		ms = types.NewMethodSet(types.NewPointer(t))
	}
	for i := range ms.Len() {
		if m := ms.At(i).Obj(); m.Exported() {
			lines = append(lines, fmt.Sprintf("  method %s%s", m.Name(), strings.TrimPrefix(types.TypeString(m.Type(), qualifier), "func")))
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
}
