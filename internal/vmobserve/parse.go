// Package vmobserve owns bounded, injected VM probe work. It never starts Docker
// or grants indexing, maintenance, notification or resource deletion authority.
package vmobserve

import (
	"errors"
	"github.com/0xmhha/indexer-go/internal/nativeobserve"
	"math"
	"strconv"
	"strings"
)

var ErrReport = errors.New("vm observation: invalid or oversized report")

const MaxStdout = 4096
const MaxStderr = 1024

func ParseFreeKiB(data []byte) (nativeobserve.Number, error) {
	bad := func() (nativeobserve.Number, error) { return nativeobserve.Number{}, ErrReport }
	if len(data) == 0 || len(data) > MaxStdout {
		return bad()
	}
	for _, b := range data {
		if b > 126 || b < 32 && b != '\n' && b != '\t' && b != '\r' {
			return bad()
		}
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if strings.ContainsRune(text, '\r') {
		return bad()
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 2 {
		return bad()
	}
	fields := strings.Fields(lines[0])
	if len(fields) != 7 || fields[0] != "Filesystem" || (fields[1] != "1024-blocks" && fields[1] != "1K-blocks") || strings.Join(fields[2:], " ") != "Used Available Capacity Mounted on" {
		return bad()
	}
	fields = strings.Fields(lines[1])
	if len(fields) != 6 || fields[5] != "/" || !strings.HasSuffix(fields[4], "%") {
		return bad()
	}
	decimal := func(s string) (uint64, error) {
		if s == "" {
			return 0, ErrReport
		}
		for _, b := range []byte(s) {
			if b < '0' || b > '9' {
				return 0, ErrReport
			}
		}
		return strconv.ParseUint(s, 10, 64)
	}
	total, e1 := decimal(fields[1])
	used, e2 := decimal(fields[2])
	free, e3 := decimal(fields[3])
	percent, e4 := decimal(strings.TrimSuffix(fields[4], "%"))
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || total == 0 || used > total || free > total-used || percent > 100 || free > math.MaxUint64/1024 {
		return bad()
	}
	return nativeobserve.Number{Present: true, Value: free * 1024}, nil
}
