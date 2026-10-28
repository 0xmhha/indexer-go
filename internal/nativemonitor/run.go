package nativemonitor

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/metadata"
)

func arguments(args []string) (policy, snapshot, at string, ok bool) {
	if len(args) > 6 || len(args)%2 != 0 {
		return "", "", "", false
	}
	seen := map[string]bool{}
	for i := 0; i < len(args); i += 2 {
		if len(args[i+1]) > 4096 {
			return "", "", "", false
		}
		name := ""
		switch args[i] {
		case "-policy", "--policy":
			name = "policy"
		case "-snapshot", "--snapshot":
			name = "snapshot"
		case "-at", "--at":
			name = "at"
		default:
			return "", "", "", false
		}
		if seen[name] {
			return "", "", "", false
		}
		seen[name] = true
		v := args[i+1]
		if v == "HOLD" {
			v = ""
		}
		switch name {
		case "policy":
			policy = v
		case "snapshot":
			snapshot = v
		case "at":
			at = v
		}
	}
	return policy, snapshot, at, true
}

// CheckFiles reads only two selected files. The exported diagnostic is inline
// data, never a request to call an exporter or traverse embedded paths.
func CheckFiles(ctx context.Context, args []string) Report {
	policy, snapshot, clock, ok := arguments(args)
	if !ok {
		r := newReport()
		r.add("cli", fail, "invalid_arguments")
		return r
	}
	reader := metadata.New(ctx)
	errorsFound := []Finding{}
	read := func(field, path string, target any) bool {
		if path == "" {
			return false
		}
		data, err := reader.File(path)
		if err != nil {
			errorsFound = append(errorsFound, Finding{field, fail, "unsafe_or_unreadable_input"})
			return false
		}
		if configfile.JSON(data, target) != nil {
			errorsFound = append(errorsFound, Finding{field, fail, "invalid_schema"})
			return false
		}
		return true
	}
	var p Policy
	var s Snapshot
	var pp *Policy
	var sp *Snapshot
	var at *time.Time
	if read("policy", policy, &p) {
		pp = &p
	}
	if read("snapshot", snapshot, &s) {
		sp = &s
	}
	if clock != "" {
		v, err := time.Parse(time.RFC3339Nano, clock)
		if err != nil {
			errorsFound = append(errorsFound, Finding{"evaluation.time", fail, "invalid_timestamp"})
		} else {
			at = &v
		}
	}
	r := Evaluate(ctx, pp, sp, at)
	for _, f := range errorsFound {
		r.add(f.Field, f.Status, f.Code)
	}
	if err := reader.Verify(); err != nil {
		if errors.Is(err, metadata.ErrChanged) {
			r.add("input", hold, "changed_during_read")
		} else {
			r.add("input", fail, "input_verification_failed")
		}
	}
	if ctx.Err() != nil {
		r.add("input", fail, "cancelled")
	}
	return r
}

func Run(ctx context.Context, args []string, out io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-help" || args[0] == "-h") {
		text := "execution-monitor --policy FILE --snapshot FILE --at RFC3339\nOffline submitted observations only; no chain, DB, RPC, watcher or actuation.\n"
		n, err := io.WriteString(out, text)
		if err != nil || n != len(text) {
			return 1
		}
		return 0
	}
	r := CheckFiles(ctx, args)
	data, err := json.Marshal(r)
	if err != nil || len(data) > 64<<10 {
		return 1
	}
	data = append(data, '\n')
	if n, err := out.Write(data); err != nil || n != len(data) || ctx.Err() != nil {
		return 1
	}
	if r.Status == fail {
		return 1
	}
	return 2
}
