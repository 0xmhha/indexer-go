package arcingress

import (
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"path/filepath"
)

func collectorConfig(c ingress.Config) error {
	c = c.Normalize()
	for _, p := range []string{c.Collector.Executable, c.Collector.SpoolDir} {
		if !filepath.IsAbs(p) || filepath.Clean(p) == string(filepath.Separator) {
			return ErrNative
		}
	}
	if c.Collector.MaxBytes <= 64<<10 || c.Collector.MaxBytes > 64<<30 || c.Collector.MaxRecords < 1 || c.Collector.MaxRecords > 1<<20 {
		return ErrNative
	}
	for _, pair := range [][2]string{{c.Native.DataDir, c.Collector.SpoolDir}, {c.Native.DataDir, c.Collector.Executable}, {c.Native.Executable, c.Collector.Executable}, {c.Native.Executable, c.Collector.SpoolDir}, {c.Collector.Executable, c.Collector.SpoolDir}} {
		if ingress.Overlap(pair[0], pair[1]) {
			return ErrNative
		}
	}
	return nil
}
