package nativeops

import (
	"encoding/json"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/x/arc/nativeinput"
	"github.com/0xmhha/indexer-go/x/arc/nativestore"
	"os"
	"path/filepath"
)

// ManagedContext is a frozen native owner, never an SQL/deletion permit.
// The trusted caller holds actual run EX; fd metadata alone proves no lock mode.
type ManagedContext struct {
	Version         uint8
	OriginalDataDir string
	Proof           nativeinput.Proof
}

func (n Native) WithRunLease(f *os.File) (Native, error) {
	if n.Managed == nil {
		return n, nil
	}
	c := *n.Managed
	if c.Proof.Receipt != nil {
		r := *c.Proof.Receipt
		c.Proof.Receipt = &r
	}
	n.Managed = &c
	n.runLease = f
	if err := n.checkRunLease(); err != nil {
		return Native{}, err
	}
	return n, nil
}
func (n Native) checkRunLease() error {
	c := n.Managed
	if c == nil || c.Version != 1 || n.Chain.ID != "5042002" || n.Chain.Genesis != c.Proof.Identity.Genesis || c.Proof.Ref != c.Proof.Head.Tip || nativeinput.ValidateProof(c.Proof, c.Proof.Identity) != nil || n.runLease == nil {
		return retention.ErrUnsafe
	}
	path := c.OriginalDataDir
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == string(os.PathSeparator) {
		return retention.ErrUnsafe
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return err
	}
	if parent != filepath.Dir(path) {
		return retention.ErrUnsafe
	}
	name := filepath.Join(parent, "."+filepath.Base(path)+".execution-retention-lock")
	if err = nativestore.PrivateFile(name); err != nil {
		return err
	}
	actual, err := os.Lstat(name)
	if err != nil {
		return err
	}
	inherited, err := n.runLease.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(actual, inherited) {
		return retention.ErrUnsafe
	}
	return nil
}
func (n Native) BindRunLease(f *os.File, p retention.Permit) (nativestore.Adapter, error) {
	if n.Managed == nil {
		return n, nil
	}
	if p.Validate() != nil || p.Visible != p.Request.ACK || p.Request.Tail != p.Request.ACK || p.Request.HeadRoot != p.Root || p.Request.Chain != n.Chain || p.Request.Head.Height != n.Managed.Proof.Head.Tip.Height || p.Request.Head.Hash != n.Managed.Proof.Head.Tip.Hash || p.Root != n.Managed.Proof.Head.Tip.Root || p.Request.Anchor.Height != n.Managed.Proof.Identity.Anchor.Height || p.Request.Anchor.Hash != n.Managed.Proof.Identity.Anchor.Hash {
		return nil, retention.ErrUnsafe
	}
	bound, err := n.WithRunLease(f)
	if err != nil {
		return nil, err
	}
	permit := p
	bound.boundPermit = &permit
	return bound, nil
}
func (n Native) managedTarget(path string) error {
	if err := n.checkRunLease(); err != nil {
		return err
	}
	original := n.Managed.OriginalDataDir
	stage := filepath.Join(filepath.Dir(original), "."+filepath.Base(original)+".execution-retention-new")
	if path != original && path != stage {
		return retention.ErrUnsafe
	}
	actual, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	if actual != path {
		return retention.ErrUnsafe
	}
	return nativeinput.VerifyCurrent(path, n.Managed.Proof, n.Managed.Proof.Identity)
}
func (n Native) writeManaged(path, operation, work string) (string, error) {
	if operation != "inspect" && operation != "hold" && operation != "prune" && operation != "verify" {
		return "", retention.ErrUnsafe
	}
	if operation != "inspect" && n.boundPermit == nil {
		return "", retention.ErrUnsafe
	}
	if err := n.managedTarget(path); err != nil {
		return "", err
	}
	b, err := json.Marshal(n.Managed)
	if err != nil || len(b) > nativeinput.MaxMeta {
		return "", retention.ErrUnsafe
	}
	output := filepath.Join(work, "managed-context.json")
	if err = os.WriteFile(output, b, 0600); err != nil {
		return "", err
	}
	return output, nil
}
