package executionops

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/stretchr/testify/require"
)

// A real subprocess consumes the inherited authority and returns an exact
// challenged receipt only if its configured backup matches that authority.
func TestRetentionReceiptChild(t *testing.T) {
	if len(os.Args) < 2 || os.Args[len(os.Args)-1] != "receipt-child" {
		return
	}
	var hello retention.Supervision
	require.NoError(t, json.NewDecoder(os.NewFile(3, "supervisor")).Decode(&hello))
	var p retention.Permit
	var expected execwire.Hash
	for i, a := range os.Args {
		if a == "-permit" {
			require.NoError(t, retention.ReadArtifact(os.Args[i+1], &p))
		}
		if a == "-config" {
			require.NoError(t, retention.ReadArtifact(os.Args[i+1], &expected))
		}
	}
	if hello.BackupDigest != expected {
		os.Exit(3)
	}
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	require.NoError(t, json.NewEncoder(os.Stdout).Encode(retention.SupervisedReply{Version: 1, Challenge: hello.Challenge, Receipt: retention.Receipt{Version: 1, PermitDigest: pd, Node: true, Spool: true}}))
	os.Exit(0)
}

func TestRetentionValidatedReceiptMutation(t *testing.T) {
	chain := execwire.Chain{ID: "5042002", Genesis: execwire.Hash{1}}
	ack := execwire.Ref{Generation: 1, Height: 132, Hash: execwire.Hash{2}}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash{3}, Token: execwire.Hash{4}, Visible: ack, Root: execwire.Hash{5}, Request: retention.Request{Version: 1, Chain: chain, Anchor: execwire.Ref{Generation: 1, Hash: chain.Genesis}, ACK: ack, Tail: ack, Head: execwire.Ref{Height: 132, Hash: ack.Hash}, HeadRoot: execwire.Hash{5}, SourceDigest: execwire.Hash{6}, KeepBlocks: 129, Through: 3}}
	require.NoError(t, p.Validate())
	pd, err := retention.Digest(p)
	require.NoError(t, err)
	a := retention.IndexerBackup{Version: 1, PermitDigest: pd, ArtifactDigest: execwire.Hash{7}, PublicationDigest: execwire.Hash{8}, DatabaseIdentity: "validated/schema", SnapshotIdentity: "snapshot-a", WALBoundary: "0/10"}
	b := a
	b.DatabaseIdentity = "foreign/schema"
	b.PublicationDigest = execwire.Hash{9}
	b.WALBoundary = "F/FFFFFFFF"
	require.NoError(t, a.Validate(p))
	require.NoError(t, b.Validate(p))
	da, err := retention.Digest(a)
	require.NoError(t, err)
	db, err := retention.Digest(b)
	require.NoError(t, err)
	t.Run("missing-authority", func(t *testing.T) {
		_, err := runRetentionProducer(t.Context(), RetentionOptions{Action: "collect", BackupReceipt: "requested", Producer: "/unused", ProducerConfig: "/unused"}, p, execwire.Hash{})
		require.ErrorIs(t, err, retention.ErrUnsafe)
		_, err = runValidatedRetentionProducer(t.Context(), RetentionOptions{Action: "restore-admit"}, p, nil)
		require.ErrorIs(t, err, retention.ErrUnsafe)
	})

	for _, tc := range []struct {
		name     string
		child    execwire.Hash
		succeeds bool
	}{{"validated-child", da, true}, {"substituted-child", db, false}} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			require.NoError(t, os.Chmod(dir, 0700))
			path := filepath.Join(dir, "receipt.json")
			config := filepath.Join(dir, "config.json")
			permit := filepath.Join(dir, "permit.json")
			producer := filepath.Join(dir, "producer")
			require.NoError(t, retention.WriteArtifact(path, a))
			require.NoError(t, retention.WriteArtifact(config, tc.child))
			require.NoError(t, retention.WriteArtifact(permit, p))
			quoted := "'" + strings.ReplaceAll(os.Args[0], "'", "'\"'\"'") + "'"
			require.NoError(t, os.WriteFile(producer, []byte("#!/bin/sh\nGORACE=atexit_sleep_ms=0 exec "+quoted+" -test.run=^TestRetentionReceiptChild$ -- \"$@\" receipt-child\n"), 0700))
			calls := 0
			_, err := runValidatedRetentionProducer(t.Context(), RetentionOptions{Action: "collect", Producer: producer, ProducerConfig: config, Permit: permit, BackupReceipt: path}, p, func(_ context.Context, got retention.Permit, external retention.IndexerBackup) error {
				calls++
				require.Equal(t, p, got)
				require.Equal(t, a, external)
				// Replace only after the live DB validator has accepted A. B is
				// structurally valid, but its DB/publication/WAL were never validated.
				require.NoError(t, os.Remove(path))
				require.NoError(t, retention.WriteArtifact(path, b))
				return nil
			})
			require.Equal(t, 1, calls)
			if tc.succeeds {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}
