package nativeops

import (
	"context"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/execwire/spool"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"path/filepath"
)

func indexerBackup(ctx context.Context, c Config, p retention.Permit) (retention.IndexerBackup, error) {
	var r retention.IndexerBackup
	if !filepath.IsAbs(c.IndexerReceipt) || !filepath.IsAbs(c.IndexerArtifact) || nativestore.PrivateFile(c.IndexerReceipt) != nil {
		return r, retention.ErrUnsafe
	}
	if err := retention.ReadArtifact(c.IndexerReceipt, &r); err != nil {
		return r, err
	}
	if err := r.Validate(p); err != nil {
		return r, err
	}
	if c.backupDigest != (execwire.Hash{}) {
		digest, err := retention.Digest(r)
		if err != nil || digest != c.backupDigest {
			return r, retention.ErrUnsafe
		}
	}
	return r, nativestore.ValidateIndexerArtifact(ctx, c.IndexerArtifact, r)
}

// The native-library verifier reconstructs the full cutoff root and compares
// every seed value; the source snapshot reopens under the exact same permit.
func verifyBackupComponents(ctx context.Context, c Config, p retention.Permit, root string) (err error) {
	native := Native{Executable: c.NativeExecutable, Chain: c.Chain, AnchorRoot: c.AnchorRoot}
	if err = native.VerifyBackup(ctx, filepath.Join(root, "native"), p); err != nil {
		return err
	}
	source := spool.Options{Path: filepath.Join(root, "source"), Chain: c.Chain, Anchor: c.Anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords}
	s, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	return s.WithRetentionSnapshot(ctx, p, func(string) error { return nil })
}

// Admission inspects supplied copies only. It never installs PostgreSQL,
// switches a native data directory, releases fences or starts services. The
// external DB receipt has already been compared with the current live E3 DB
// by the inherited supervisor before this function is reached from the CLI.
func admitRestore(ctx context.Context, c Config, p retention.Permit) (r retention.Receipt, err error) {
	external, err := indexerBackup(ctx, c, p)
	if err != nil {
		return r, err
	}
	manifest, err := nativestore.ValidateMatchedBackup(ctx, c.RestoreManifest, p, external, p.Visible)
	if err != nil {
		return r, err
	}
	root := filepath.Join(filepath.Dir(c.RestoreManifest), manifest.ComponentsDir)
	if err = verifyBackupComponents(ctx, c, p, root); err != nil {
		return r, err
	}
	source := spool.Options{Path: filepath.Join(root, "source"), Chain: c.Chain, Anchor: c.Anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords}
	s, err := spool.OpenMaintenance(ctx, source, p)
	if err != nil {
		return r, err
	}
	defer func() { err = errors.Join(err, s.Close()) }()
	return s.RetentionCompletion(ctx, p)
}

func (n Native) VerifyBackup(ctx context.Context, path string, p retention.Permit) error {
	if p.Validate() != nil || p.Request.Chain != n.Chain {
		return retention.ErrUnsafe
	}
	digest, err := retention.Digest(p)
	if err != nil {
		return err
	}
	var reply struct{ Status string }
	input := struct {
		Apply   apply
		Visible execwire.Ref
		Root    execwire.Hash
	}{apply{p.Request, digest}, p.Visible, p.Root}
	if err = n.run(ctx, path, "match-backup", input, &reply); err != nil {
		return err
	}
	if reply.Status != "native_verified" {
		return retention.ErrUnsafe
	}
	return nil
}
