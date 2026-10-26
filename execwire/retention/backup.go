package retention

import (
	"github.com/0xmhha/indexer-go/execwire"
	"regexp"
)

// IndexerBackup is an externally authenticated PostgreSQL snapshot receipt.
// It binds an external DB backup/WAL boundary; this package never restores SQL.
type IndexerBackup struct {
	Version                                         uint16
	PermitDigest, ArtifactDigest, PublicationDigest execwire.Hash
	DatabaseIdentity, SnapshotIdentity, WALBoundary string
}

func (r IndexerBackup) Validate(p Permit) error {
	pd, err := Digest(p)
	if err != nil || p.Validate() != nil || r.Version != 1 || r.PermitDigest != pd ||
		r.ArtifactDigest == (execwire.Hash{}) || r.PublicationDigest == (execwire.Hash{}) ||
		len(r.DatabaseIdentity) == 0 || len(r.DatabaseIdentity) > 256 ||
		len(r.SnapshotIdentity) == 0 || len(r.SnapshotIdentity) > 256 ||
		!regexp.MustCompile(`^[0-9A-F]{1,8}/[0-9A-F]{1,8}$`).MatchString(r.WALBoundary) {
		return ErrUnsafe
	}
	return nil
}
