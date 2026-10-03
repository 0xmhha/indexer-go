package testchain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/cockroachdb/pebble"
)

// Entry is one key/value pair of a dumped database.
type Entry struct {
	Key   []byte
	Value []byte
}

// Filter decides how a stored entry appears in a dump: it returns the value to
// record (possibly normalized) and whether to keep the entry at all.
type Filter func(key, value []byte) ([]byte, bool)

// DumpKeyspace opens the Pebble database at path read-only and returns every
// key/value pair in key order, passed through filter (nil keeps everything).
// The database must not be open elsewhere.
func DumpKeyspace(path string, filter Filter) ([]Entry, error) {
	db, err := pebble.Open(path, &pebble.Options{ReadOnly: true, Logger: quietLogger{}})
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer db.Close()

	it, err := db.NewIter(nil)
	if err != nil {
		return nil, err
	}
	defer it.Close()

	var out []Entry
	for it.First(); it.Valid(); it.Next() {
		k := bytes.Clone(it.Key())
		v, err := it.ValueAndErr()
		if err != nil {
			return nil, err
		}
		v = bytes.Clone(v)
		if filter != nil {
			var keep bool
			if v, keep = filter(k, v); !keep {
				continue
			}
		}
		out = append(out, Entry{Key: k, Value: v})
	}
	return out, it.Error()
}

// FormatKeyspace writes one line per entry: printable key, value length and
// a short value digest. The output is stable and diffs line by line.
func FormatKeyspace(w io.Writer, entries []Entry) error {
	for _, e := range entries {
		sum := sha256.Sum256(e.Value)
		if _, err := fmt.Fprintf(w, "%s %d %s\n", printableKey(e.Key), len(e.Value), hex.EncodeToString(sum[:8])); err != nil {
			return err
		}
	}
	return nil
}

// DiffKeyspace reports keys that are missing, extra or different between
// want and got, up to max lines (0 = unlimited).
func DiffKeyspace(want, got []Entry, max int) []string {
	var out []string
	add := func(s string) bool {
		out = append(out, s)
		return max == 0 || len(out) < max
	}
	i, j := 0, 0
	for i < len(want) || j < len(got) {
		switch {
		case j >= len(got) || (i < len(want) && bytes.Compare(want[i].Key, got[j].Key) < 0):
			if !add("- " + printableKey(want[i].Key)) {
				return out
			}
			i++
		case i >= len(want) || bytes.Compare(want[i].Key, got[j].Key) > 0:
			if !add("+ " + printableKey(got[j].Key)) {
				return out
			}
			j++
		default:
			if !bytes.Equal(want[i].Value, got[j].Value) {
				if !add("~ " + printableKey(want[i].Key)) {
					return out
				}
			}
			i++
			j++
		}
	}
	return out
}

// WriteKeyspaceFile writes FormatKeyspace output to path.
func WriteKeyspaceFile(path string, entries []Entry) error {
	var buf bytes.Buffer
	if err := FormatKeyspace(&buf, entries); err != nil {
		return err
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// printableKey keeps ASCII key parts readable and hex-encodes binary runs.
func printableKey(k []byte) string {
	var b strings.Builder
	inHex := false
	for _, c := range k {
		if c < 0x80 && unicode.IsPrint(rune(c)) && c != ' ' && c != '\\' {
			if inHex {
				b.WriteByte('>')
				inHex = false
			}
			b.WriteByte(c)
			continue
		}
		if !inHex {
			b.WriteString("<")
			inHex = true
		}
		fmt.Fprintf(&b, "%02x", c)
	}
	if inHex {
		b.WriteByte('>')
	}
	return b.String()
}

// quietLogger drops Pebble's informational WAL replay messages.
type quietLogger struct{}

func (quietLogger) Infof(string, ...interface{})  {}
func (quietLogger) Errorf(string, ...interface{}) {}
func (quietLogger) Fatalf(format string, args ...interface{}) {
	panic(fmt.Sprintf(format, args...))
}

// SummarizeDiff groups DiffKeyspace lines by the first two key path segments
// (for example "/index/balance/") and counts missing (-), extra (+) and
// changed (~) keys per group.
func SummarizeDiff(lines []string) []string {
	type counts struct{ missing, extra, changed int }
	groups := map[string]*counts{}
	var order []string
	for _, l := range lines {
		if len(l) < 3 {
			continue
		}
		key := l[2:]
		parts := strings.SplitN(key, "/", 4)
		group := key
		if len(parts) >= 3 {
			group = "/" + parts[1] + "/" + parts[2] + "/"
		}
		c, ok := groups[group]
		if !ok {
			c = &counts{}
			groups[group] = c
			order = append(order, group)
		}
		switch l[0] {
		case '-':
			c.missing++
		case '+':
			c.extra++
		case '~':
			c.changed++
		}
	}
	out := make([]string, 0, len(order))
	for _, g := range order {
		c := groups[g]
		out = append(out, fmt.Sprintf("%s missing=%d extra=%d changed=%d", g, c.missing, c.extra, c.changed))
	}
	return out
}
