//go:build darwin || linux

package metadata

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestForeignOwnershipAndHardlinksRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "meta")
	if e := os.WriteFile(p, []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	i, e := os.Stat(p)
	if e != nil {
		t.Fatal(e)
	}
	st := i.Sys().(*syscall.Stat_t)
	st.Uid = uint32(os.Geteuid()) + 1
	if owned(i, true) {
		t.Fatal("foreign owner accepted")
	}
	st.Uid = uint32(os.Geteuid())
	st.Nlink = 2
	if owned(i, true) {
		t.Fatal("hard link accepted")
	}
}
