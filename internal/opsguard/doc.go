// Package opsguard stops the indexer before shared storage is exhausted and
// keeps it stopped until an operator resumes it.
//
// A Monitor samples filesystem free space, the PostgreSQL data directory and
// raw window usage, and halts the process on a stop threshold, a forecast that
// reaches the floor too soon, or a missing observation. A halt writes a latch
// file; startup refuses to run while it exists, and only Resume, which samples
// the resume thresholds again, removes it. Classify tells restart logic which
// errors must not be retried automatically.
//
// The package makes no RPC calls, opens no database connection and never
// deletes indexed data or raw inputs to recover space.
package opsguard
