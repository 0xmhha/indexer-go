package nativestore

import "os"

// AcquireExclusiveRun excludes both importer and RO collector. It starts no
// child and grants no deletion authority; the existing current E3 lease is
// separately required. Acquire before entering any SQL transaction.
func AcquireExclusiveRun(dataDir string) (*os.File, error) { return acquireRun(dataDir, takeLease) }
