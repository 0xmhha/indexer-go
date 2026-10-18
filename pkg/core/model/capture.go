package model

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
)

// Capture values are independent of peer protocols, database layouts and
// projection state. Validation is not a durable commit or a pruning ACK.

var (
	ErrCaptureInvalid    = errors.New("invalid capture data")
	ErrCaptureIncomplete = errors.New("capture is incomplete")
)

// FeedChain distinguishes networks that reuse the same chain ID.
type FeedChain struct {
	ID          string      `json:"id"` // canonical uint256 decimal, including chain ID 0
	GenesisHash common.Hash `json:"genesisHash"`
}

func (c FeedChain) Validate() error {
	if len(c.ID) == 0 || len(c.ID) > 78 || (len(c.ID) > 1 && c.ID[0] == '0') || c.GenesisHash == (common.Hash{}) {
		return fmt.Errorf("%w: chain identity", ErrCaptureInvalid)
	}
	for _, r := range c.ID {
		if r < '0' || r > '9' {
			return fmt.Errorf("%w: chain ID must be decimal", ErrCaptureInvalid)
		}
	}
	n, ok := new(big.Int).SetString(c.ID, 10)
	if !ok || n.BitLen() > 256 {
		return fmt.Errorf("%w: chain ID exceeds uint256", ErrCaptureInvalid)
	}
	return nil
}

// FeedBlock identifies immutable input. Canonicality is checked separately.
type FeedBlock struct {
	Chain  FeedChain   `json:"chain"`
	Number uint64      `json:"number"`
	Hash   common.Hash `json:"hash"`
}

func (b FeedBlock) Validate() error {
	if err := b.Chain.Validate(); err != nil {
		return err
	}
	if b.Hash == (common.Hash{}) || (b.Number == 0 && b.Hash != b.Chain.GenesisHash) {
		return fmt.Errorf("%w: block identity", ErrCaptureInvalid)
	}
	return nil
}

type DatasetKind string

const (
	DatasetHeaders      DatasetKind = "headers"
	DatasetBodies       DatasetKind = "bodies"
	DatasetTransactions DatasetKind = "transactions"
	DatasetReceipts     DatasetKind = "receipts"
	DatasetLogs         DatasetKind = "logs"
	DatasetAccounts     DatasetKind = "account_changes"
	DatasetCode         DatasetKind = "contract_code"
	DatasetStorage      DatasetKind = "storage_changes"
	DatasetTraces       DatasetKind = "traces"
	DatasetBlobs        DatasetKind = "blobs"
	DatasetStateInputs  DatasetKind = "state_inputs"
)

func (k DatasetKind) Valid() bool {
	switch k {
	case DatasetHeaders, DatasetBodies, DatasetTransactions, DatasetReceipts, DatasetLogs,
		DatasetAccounts, DatasetCode, DatasetStorage, DatasetTraces, DatasetBlobs, DatasetStateInputs:
		return true
	default:
		return false
	}
}

// CoverageComplete means the entire dataset for a block, not execution/proof
// verification. A tracked selection is hashed from its canonical definition;
// two different address/slot sets must not satisfy each other's requirement.
type DatasetCoverage struct {
	Scope         string      `json:"scope"`
	SelectionHash common.Hash `json:"selectionHash"`
}

const (
	CoverageComplete = "complete"
	CoverageTracked  = "tracked"
)

func (c DatasetCoverage) Validate() error {
	if (c.Scope == CoverageComplete && c.SelectionHash == (common.Hash{})) ||
		(c.Scope == CoverageTracked && c.SelectionHash != (common.Hash{})) {
		return nil
	}
	return fmt.Errorf("%w: coverage scope/selection", ErrCaptureInvalid)
}

func (c DatasetCoverage) Covers(required DatasetCoverage) bool {
	return c.Validate() == nil && required.Validate() == nil &&
		(c.Scope == CoverageComplete || c == required)
}

type DatasetRequirement struct {
	Kind     DatasetKind     `json:"kind"`
	Coverage DatasetCoverage `json:"coverage"`
}

// CapturePlan is immutable once used. The content digest binds the exact
// required datasets and selections, even if someone reuses a version number.
type CapturePlan struct {
	Version  uint32               `json:"version"`
	Required []DatasetRequirement `json:"required"`
}

func (p CapturePlan) Validate() error {
	if p.Version == 0 || len(p.Required) == 0 || len(p.Required) > 11 {
		return fmt.Errorf("%w: plan version or requirements", ErrCaptureInvalid)
	}
	seen := make(map[DatasetKind]bool, len(p.Required))
	for _, r := range p.Required {
		if !r.Kind.Valid() || seen[r.Kind] {
			return fmt.Errorf("%w: unknown or duplicate dataset %q", ErrCaptureInvalid, r.Kind)
		}
		if err := r.Coverage.Validate(); err != nil {
			return err
		}
		seen[r.Kind] = true
	}
	return nil
}

// Digest is independent of requirement order and never sorts caller memory.
func (p CapturePlan) Digest() (common.Hash, error) {
	if err := p.Validate(); err != nil {
		return common.Hash{}, err
	}
	p.Required = append([]DatasetRequirement(nil), p.Required...)
	sort.Slice(p.Required, func(i, j int) bool { return p.Required[i].Kind < p.Required[j].Kind })
	data, err := json.Marshal(p)
	if err != nil {
		return common.Hash{}, err
	}
	return sha256.Sum256(append([]byte("indexer.capture.plan.v1\x00"), data...)), nil
}

type DatasetStatus string

const (
	DatasetMissing       DatasetStatus = "missing"
	DatasetFetching      DatasetStatus = "fetching"
	DatasetCaptured      DatasetStatus = "captured"
	DatasetVerified      DatasetStatus = "verified"
	DatasetPersisted     DatasetStatus = "persisted"
	DatasetVerifiedEmpty DatasetStatus = "verified_empty"
	DatasetNotApplicable DatasetStatus = "not_applicable"
	DatasetUnsupported   DatasetStatus = "unsupported"
	DatasetFailed        DatasetStatus = "failed"
	DatasetPruned        DatasetStatus = "pruned"
)

type CaptureVerification string

const (
	VerificationCommitment CaptureVerification = "commitment"
	VerificationProof      CaptureVerification = "proof"
	VerificationExecution  CaptureVerification = "execution"
	VerificationHashBound  CaptureVerification = "source_hash_bound"
	VerificationForkRules  CaptureVerification = "fork_rules"
)

// DatasetEvidence records coverage and provenance; hash binding alone is not
// a state proof. Every entry belongs to its enclosing manifest's block hash.
type DatasetEvidence struct {
	Kind         DatasetKind         `json:"kind"`
	Coverage     DatasetCoverage     `json:"coverage"`
	Status       DatasetStatus       `json:"status"`
	Digest       common.Hash         `json:"digest"` // digest of exact encoded dataset, including proven empty
	Codec        string              `json:"codec"`
	Source       string              `json:"source"`
	Verification CaptureVerification `json:"verification"`
	Reason       string              `json:"reason,omitempty"`
}

// CaptureIdentity fences work across forks and capture plan changes.
type CaptureIdentity struct {
	Block       FeedBlock   `json:"block"`
	Generation  uint64      `json:"generation"`
	PlanVersion uint32      `json:"planVersion"`
	PlanDigest  common.Hash `json:"planDigest"`
}

type CaptureManifest struct {
	Identity CaptureIdentity   `json:"identity"`
	Datasets []DatasetEvidence `json:"datasets"`
}

// ValidateComplete checks evidence for a complete manifest. Only a storage
// transaction can make it durable; this method never authorizes ACK or GC.
func (p CapturePlan) ValidateComplete(m CaptureManifest) error {
	digest, err := p.Digest()
	if err != nil {
		return err
	}
	if err := m.Identity.Block.Validate(); err != nil {
		return err
	}
	if m.Identity.Generation == 0 || m.Identity.PlanVersion != p.Version || m.Identity.PlanDigest != digest {
		return fmt.Errorf("%w: manifest identity or plan mismatch", ErrCaptureInvalid)
	}
	if len(m.Datasets) != len(p.Required) {
		return fmt.Errorf("%w: dataset count", ErrCaptureIncomplete)
	}
	seen := make(map[DatasetKind]DatasetEvidence, len(m.Datasets))
	for _, e := range m.Datasets {
		if _, duplicate := seen[e.Kind]; duplicate || !e.Kind.Valid() {
			return fmt.Errorf("%w: duplicate or unknown evidence %q", ErrCaptureInvalid, e.Kind)
		}
		seen[e.Kind] = e
	}
	for _, r := range p.Required {
		e, ok := seen[r.Kind]
		if !ok || !e.Coverage.Covers(r.Coverage) {
			return fmt.Errorf("%w: %s coverage", ErrCaptureIncomplete, r.Kind)
		}
		if e.Source == "" || e.Codec == "" {
			return fmt.Errorf("%w: %s provenance", ErrCaptureIncomplete, r.Kind)
		}
		switch e.Status {
		case DatasetPersisted, DatasetVerifiedEmpty:
			if e.Digest == (common.Hash{}) {
				return fmt.Errorf("%w: %s content digest", ErrCaptureIncomplete, r.Kind)
			}
			switch e.Verification {
			case VerificationCommitment, VerificationProof, VerificationExecution, VerificationHashBound:
			default:
				return fmt.Errorf("%w: %s verification", ErrCaptureIncomplete, r.Kind)
			}
		case DatasetNotApplicable:
			if e.Verification != VerificationForkRules || e.Reason == "" {
				return fmt.Errorf("%w: %s fork applicability", ErrCaptureIncomplete, r.Kind)
			}
		default:
			return fmt.Errorf("%w: %s status %s", ErrCaptureIncomplete, r.Kind, e.Status)
		}
	}
	return nil
}
