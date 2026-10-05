// Package replay records the JSON-RPC traffic between the indexer and a node
// into files and serves it back, so indexing can be rerun without the node
// (development, reproduction, benchmarks). Everything the indexer asks the
// node is replayed: blocks and receipts, but also balances and eth_call, so a
// replayed run stores exactly what the recorded run stored.
//
// Archive layout:
//
//	manifest.json                    format version and recorded block range
//	blocks/<first height>.jsonl[.gz] block-addressed calls, 1,000 heights per file
//	calls.jsonl[.gz]                 every other call
//
// Each line is one call: {"m":method,"p":params,"r":result} or with "e" (the
// JSON-RPC error) instead of "r". Responses are kept exactly as the node sent
// them, so the archive is chain-independent. Files are written uncompressed
// while recording; Compress gzips them, and readers accept both.
package replay

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/ethereum/go-ethereum/common/hexutil"
)

// FormatVersion is the archive format written by this package.
const FormatVersion = 1

// segmentSize is the number of heights per block file.
const segmentSize = 1000

// call is one recorded JSON-RPC call.
type call struct {
	Method string          `json:"m"`
	Params json.RawMessage `json:"p"`
	Result json.RawMessage `json:"r,omitempty"`
	Error  json.RawMessage `json:"e,omitempty"`
}

// Manifest describes an archive.
type Manifest struct {
	Version int    `json:"version"`
	First   uint64 `json:"first"` // lowest recorded block height
	Last    uint64 `json:"last"`  // highest recorded block height
	Blocks  int    `json:"blocks"`
}

// blockMethods are calls whose first parameter is a block number; they are
// stored by height so a replay loads only the file it needs.
var blockMethods = map[string]bool{
	"eth_getBlockByNumber": true,
	"eth_getBlockReceipts": true,
}

// key identifies a call by method and parameters (compacted JSON).
func key(method string, params json.RawMessage) string {
	var buf bytes.Buffer
	if t := bytes.TrimSpace(params); len(t) == 0 || string(t) == "null" {
		params = json.RawMessage("[]") // no parameters, however sent
	}
	if err := json.Compact(&buf, params); err != nil {
		buf.Reset()
		buf.Write(params)
	}
	return method + "|" + buf.String()
}

// blockHeight returns the block number a block-addressed call refers to, if
// its first parameter is a hex number (not a tag such as "latest").
func blockHeight(method string, params json.RawMessage) (uint64, bool) {
	if !blockMethods[method] {
		return 0, false
	}
	var ps []json.RawMessage
	if json.Unmarshal(params, &ps) != nil || len(ps) == 0 {
		return 0, false
	}
	var s string
	if json.Unmarshal(ps[0], &s) != nil {
		return 0, false
	}
	n, err := hexutil.DecodeUint64(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

func segmentOf(h uint64) uint64 { return h / segmentSize * segmentSize }

func segmentName(start uint64) string { return fmt.Sprintf("%020d.jsonl", start) }

// ---------------------------------------------------------------------------
// Writing
// ---------------------------------------------------------------------------

// Writer appends calls to an archive directory. It is safe for concurrent
// use. Close writes the manifest.
type Writer struct {
	dir string

	mu       sync.Mutex
	files    map[string]*os.File
	bufs     map[string]*bufio.Writer
	first    uint64
	last     uint64
	heights  map[uint64]bool
	anyBlock bool
	closed   bool
}

// NewWriter creates (or extends) an archive in dir.
func NewWriter(dir string) (*Writer, error) {
	if err := os.MkdirAll(filepath.Join(dir, "blocks"), 0o755); err != nil {
		return nil, err
	}
	w := &Writer{dir: dir, files: map[string]*os.File{}, bufs: map[string]*bufio.Writer{}, heights: map[uint64]bool{}}
	if m, err := readManifest(dir); err == nil && m.Blocks > 0 {
		w.first, w.last, w.anyBlock = m.First, m.Last, true
	}
	return w, nil
}

// Record appends one call. Calls with a block tag instead of a number
// ("latest") and eth_blockNumber are not recorded: a replay answers them
// from the recorded range.
func (w *Writer) Record(method string, params, result, rpcErr json.RawMessage) error {
	if method == "eth_blockNumber" {
		return nil
	}
	file := "calls.jsonl"
	h, byHeight := blockHeight(method, params)
	if blockMethods[method] && !byHeight {
		return nil // tag: depends on when it was asked
	}
	if byHeight {
		file = filepath.Join("blocks", segmentName(segmentOf(h)))
	}
	line, err := json.Marshal(call{Method: method, Params: params, Result: result, Error: rpcErr})
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return errors.New("replay: writer closed")
	}
	bw, err := w.open(file)
	if err != nil {
		return err
	}
	if _, err := bw.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if byHeight && method == "eth_getBlockByNumber" && len(result) > 0 && string(result) != "null" {
		if !w.anyBlock || h < w.first {
			w.first = h
		}
		if !w.anyBlock || h > w.last {
			w.last = h
		}
		w.anyBlock = true
		w.heights[h] = true
	}
	return nil
}

func (w *Writer) open(name string) (*bufio.Writer, error) {
	if bw, ok := w.bufs[name]; ok {
		return bw, nil
	}
	f, err := os.OpenFile(filepath.Join(w.dir, name), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w.files[name] = f
	w.bufs[name] = bufio.NewWriterSize(f, 64<<10)
	return w.bufs[name], nil
}

// Close flushes and closes the files and writes the manifest.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	var errs []error
	for name, bw := range w.bufs {
		errs = append(errs, bw.Flush(), w.files[name].Close())
	}
	m := Manifest{Version: FormatVersion}
	if w.anyBlock {
		m.First, m.Last = w.first, w.last
		prev, _ := readManifest(w.dir)
		m.Blocks = len(w.heights)
		if prev.Blocks > m.Blocks {
			m.Blocks = prev.Blocks
		}
	}
	errs = append(errs, writeManifest(w.dir, m))
	return errors.Join(errs...)
}

func readManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return m, err
	}
	err = json.Unmarshal(b, &m)
	return m, err
}

func writeManifest(dir string, m Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "manifest.json"), append(b, '\n'), 0o644)
}

// Compress gzips every archive file in dir (x.jsonl → x.jsonl.gz).
func Compress(dir string) error {
	files, err := archiveFiles(dir)
	if err != nil {
		return err
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".gz") {
			continue
		}
		in, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := zw.Write(in); err != nil {
			return err
		}
		if err := zw.Close(); err != nil {
			return err
		}
		if err := os.WriteFile(f+".gz", buf.Bytes(), 0o644); err != nil {
			return err
		}
		if err := os.Remove(f); err != nil {
			return err
		}
	}
	return nil
}

func archiveFiles(dir string) ([]string, error) {
	var out []string
	for _, pattern := range []string{"calls.jsonl*", filepath.Join("blocks", "*.jsonl*")} {
		m, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, err
		}
		out = append(out, m...)
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// Reading
// ---------------------------------------------------------------------------

// Archive reads a recorded archive. Block files are loaded on demand.
type Archive struct {
	dir      string
	manifest Manifest
	segments map[uint64]string // segment start -> file path

	mu     sync.Mutex
	calls  map[string]call
	loaded map[uint64]map[string]call // segment start -> calls
}

// Open opens an archive directory.
func Open(dir string) (*Archive, error) {
	a := &Archive{dir: dir, segments: map[uint64]string{}, calls: map[string]call{}, loaded: map[uint64]map[string]call{}}
	files, err := filepath.Glob(filepath.Join(dir, "blocks", "*.jsonl*"))
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		base := strings.TrimSuffix(strings.TrimSuffix(filepath.Base(f), ".gz"), ".jsonl")
		start, err := strconv.ParseUint(base, 10, 64)
		if err != nil {
			continue
		}
		a.segments[start] = f
	}
	for _, name := range []string{"calls.jsonl", "calls.jsonl.gz"} {
		if err := readCalls(filepath.Join(dir, name), a.calls); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	if m, err := readManifest(dir); err == nil {
		a.manifest = m
	} else {
		if err := a.scanRange(); err != nil {
			return nil, err
		}
	}
	if a.manifest.Version > FormatVersion {
		return nil, fmt.Errorf("replay: archive format %d is newer than %d", a.manifest.Version, FormatVersion)
	}
	return a, nil
}

// scanRange derives the block range from the files when there is no
// manifest (a recording that did not close cleanly).
func (a *Archive) scanRange() error {
	found := false
	for start := range a.segments {
		calls, err := a.segment(start)
		if err != nil {
			return err
		}
		for _, c := range calls {
			h, ok := blockHeight(c.Method, c.Params)
			if !ok || c.Method != "eth_getBlockByNumber" || len(c.Result) == 0 || string(c.Result) == "null" {
				continue
			}
			if !found || h < a.manifest.First {
				a.manifest.First = h
			}
			if !found || h > a.manifest.Last {
				a.manifest.Last = h
			}
			found = true
			a.manifest.Blocks++
		}
	}
	a.manifest.Version = FormatVersion
	return nil
}

// Manifest returns the archive's description.
func (a *Archive) Manifest() Manifest { return a.manifest }

// Segments returns the starts of the block files, in order.
func (a *Archive) Segments() []uint64 {
	out := make([]uint64, 0, len(a.segments))
	for s := range a.segments {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// lookup returns the recorded call for method and params.
func (a *Archive) lookup(method string, params json.RawMessage) (call, bool, error) {
	k := key(method, params)
	if h, ok := blockHeight(method, params); ok {
		calls, err := a.segment(segmentOf(h))
		if err != nil {
			return call{}, false, err
		}
		c, ok := calls[k]
		return c, ok, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	c, ok := a.calls[k]
	return c, ok, nil
}

// segment returns the calls of one block file, loading it on first use.
func (a *Archive) segment(start uint64) (map[string]call, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if calls, ok := a.loaded[start]; ok {
		return calls, nil
	}
	calls := map[string]call{}
	if path, ok := a.segments[start]; ok {
		if err := readCalls(path, calls); err != nil {
			return nil, err
		}
	}
	a.loaded[start] = calls
	return calls, nil
}

// readCalls reads a (possibly gzipped) JSONL file into calls; later lines
// replace earlier ones for the same call.
func readCalls(path string, into map[string]call) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }() // read-only
	var r io.Reader = f
	if strings.HasSuffix(path, ".gz") {
		zr, err := gzip.NewReader(f)
		if err != nil {
			return fmt.Errorf("replay: %s: %w", path, err)
		}
		// Corrupt data surfaces as a read error through the scanner.
		defer func() { _ = zr.Close() }()
		r = zr
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 256<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var c call
		if err := json.Unmarshal(line, &c); err != nil {
			// A recording cut short by a crash may end in a partial line.
			continue
		}
		into[key(c.Method, c.Params)] = c
	}
	return sc.Err()
}
