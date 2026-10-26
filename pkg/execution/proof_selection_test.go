package execution

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecutionProofSelectionPrivateStrictAndCancel(t *testing.T) {
	d := t.TempDir()
	if e := os.Chmod(d, 0700); e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(d, "selection.json")
	good := `{"targets":[{"address":"0x0000000000000000000000000000000000000001","code":true,"slots":[]}]}`
	if e := os.WriteFile(p, []byte(good), 0600); e != nil {
		t.Fatal(e)
	}
	s, e := ReadStateProofSelection(t.Context(), p)
	if e != nil || len(s.Targets) != 1 || !s.Targets[0].Code {
		t.Fatalf("private valid selection refused: %v", e)
	}
	for _, raw := range []string{strings.Replace(good, `"code":true`, `"code":true,"code":false`, 1), strings.Replace(good, `"code":true`, `"unknown":true`, 1), `{"targets":[]}`, good + good, strings.Repeat(" ", 16385)} {
		if e = os.WriteFile(p, []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		if _, e = ReadStateProofSelection(t.Context(), p); e == nil {
			t.Fatal("invalid selection accepted")
		}
	}
	_ = os.WriteFile(p, []byte(good), 0600)
	_ = os.Chmod(p, 0644)
	if _, e = ReadStateProofSelection(t.Context(), p); e == nil {
		t.Fatal("public selection accepted")
	}
	_ = os.Chmod(p, 0600)
	link := filepath.Join(d, "link")
	_ = os.Symlink(p, link)
	if _, e = ReadStateProofSelection(t.Context(), link); e == nil {
		t.Fatal("symlink selection accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, e = ReadStateProofSelection(ctx, p); !errors.Is(e, context.Canceled) {
		t.Fatal("cancel ignored", e)
	}
}
