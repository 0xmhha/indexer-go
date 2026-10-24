package checkpoint

import (
	"bytes"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"io"
	"strings"
	"testing"
)

type shortWriter struct{}

func (shortWriter) Write(b []byte) (int, error) { return len(b) - 1, nil }
func TestStreamBoundsAndStrictDecode(t *testing.T) {
	var b bytes.Buffer
	want := Record{Kind: "end"}
	if err := Write(&b, want); err != nil {
		t.Fatal(err)
	}
	d := NewDecoder(&b)
	v, err := d.Next()
	if err != nil || v.Kind != want.Kind {
		t.Fatal(v, err)
	}
	if _, err = d.Next(); err != io.EOF {
		t.Fatal(err)
	}
	for _, raw := range []string{`{"Kind":"end","unknown":1}`, `{"Kind":"end"} {}`, strings.Repeat(" ", MaxRecordBytes+1)} {
		if _, err = NewDecoder(strings.NewReader(raw)).Next(); err == nil {
			t.Fatal("invalid record accepted")
		}
	}
	if err = Write(shortWriter{}, want); err != io.ErrShortWrite {
		t.Fatal(err)
	}
	if err = Write(io.Discard, Record{Kind: "account", Code: make([]byte, MaxRecordBytes)}); !errors.Is(err, execwire.ErrLimit) {
		t.Fatal(err)
	}
}
