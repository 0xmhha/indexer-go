package configfile

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStrictJSONBoundsTypesAndOptionalPointers(t *testing.T) {
	type data struct {
		Value    uint64
		Optional *int
	}
	for _, b := range []string{`{"Value":1,"Optional":null}`, `{"Value":18446744073709551615}`} {
		var v data
		if err := JSON([]byte(b), &v); err != nil {
			t.Fatal(b, err)
		}
	}
	for _, b := range []string{`{"Value":1,"value":2}`, `{"Value":null}`, `{"Unknown":1}`, `{"Value":"HOLD"}`, `{"Value":1} {}`, `{"Value":`, `null`, string(bytes.Repeat([]byte("["), 34)) + string(bytes.Repeat([]byte("]"), 34))} {
		var v data
		if err := JSON([]byte(b), &v); err == nil {
			t.Fatalf("accepted %s", b)
		}
	}
	var v data
	if err := JSON(bytes.Repeat([]byte(" "), MaxBytes+1), &v); err == nil {
		t.Fatal("oversized input accepted")
	}
}
func TestReadRefusesFileAndParentLinksAndDirectories(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "config.json")
	if err = os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.json")
	if err = os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	parent := filepath.Join(dir, "parent")
	if err = os.Symlink(dir, parent); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{link, filepath.Join(parent, "config.json"), dir, filepath.Join(dir, "absent")} {
		if _, err := Read(p); err == nil {
			t.Fatal("unsafe input accepted")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("input modified")
	}
}
