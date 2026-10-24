package retention

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactsArePrivateExclusiveBoundedAndStrict(t *testing.T) {
	dir := t.TempDir()
	if e := os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "request.json")
	r := validRequest()
	if e := WriteArtifact(path, r); e != nil {
		t.Fatal("offline operator must be able to persist an exact artifact:", e)
	}
	var got Request
	if e := ReadArtifact(path, &got); e != nil || got != r {
		t.Fatal("artifact changed", got, e)
	}
	info, e := os.Stat(path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("artifact not private", e)
	}
	if e = WriteArtifact(path, r); e == nil {
		t.Fatal("existing evidence overwritten")
	}
	for _, data := range []string{`{"Version":1,"unknown":true}`, `{} {}`, `{"Version":`} {
		if e = os.WriteFile(path, []byte(data), 0600); e != nil {
			t.Fatal(e)
		}
		if ReadArtifact(path, &got) == nil {
			t.Fatal("malformed/trailing/unknown JSON accepted", data)
		}
	}
	if e = os.WriteFile(path, make([]byte, MaxArtifact+1), 0600); e != nil {
		t.Fatal(e)
	}
	if ReadArtifact(path, &got) == nil {
		t.Fatal("oversized artifact accepted")
	}
}
