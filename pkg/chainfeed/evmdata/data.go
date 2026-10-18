// Package evmdata validates encoded execution inputs without running an EVM.
// State transitions, consensus finality and blob sidecars are separate inputs.
package evmdata

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/pkg/window"
)

const HeaderCodec = "eth.header.rlp.v1"
const BodyCodec = "eth.body.rlp.v1"
const ReceiptsCodec = "eth.receipts68.rlp.v1"
const MaxItems = 32768

func BasePlan() model.CapturePlan {
	p := model.CapturePlan{Version: 1}
	for _, k := range []model.DatasetKind{model.DatasetHeaders, model.DatasetBodies, model.DatasetReceipts} {
		p.Required = append(p.Required, model.DatasetRequirement{Kind: k, Coverage: model.DatasetCoverage{Scope: model.CoverageComplete}})
	}
	return p
}
func Capabilities(chain model.FeedChain) chainfeed.Capabilities {
	c := chainfeed.Capabilities{Chain: chain}
	for _, r := range BasePlan().Required {
		c.Datasets = append(c.Datasets, chainfeed.Capability{Dataset: r.Kind, Coverage: r.Coverage})
	}
	return c
}
func Codec(k model.DatasetKind) string {
	switch k {
	case model.DatasetHeaders:
		return HeaderCodec
	case model.DatasetBodies:
		return BodyCodec
	case model.DatasetReceipts:
		return ReceiptsCodec
	}
	return ""
}

// Items bounds list cardinality before profile decoders allocate slices.
func Items(raw []byte, max int) ([]rlp.RawValue, error) {
	content, rest, err := rlp.SplitList(raw)
	if err != nil || len(rest) != 0 {
		return nil, errors.New("evmdata: malformed RLP list")
	}
	var out []rlp.RawValue
	for len(content) > 0 {
		if len(out) >= max {
			return nil, errors.New("evmdata: list cardinality exceeds limit")
		}
		_, _, tail, err := rlp.Split(content)
		if err != nil {
			return nil, err
		}
		out = append(out, content[:len(content)-len(tail)])
		content = tail
	}
	return out, nil
}

type trieValues [][]byte

func (v trieValues) Len() int                           { return len(v) }
func (v trieValues) EncodeIndex(i int, b *bytes.Buffer) { b.Write(v[i]) }
func Root(raw []byte, typed bool) (common.Hash, error) {
	items, err := Items(raw, MaxItems)
	if err != nil {
		return common.Hash{}, err
	}
	values := make(trieValues, len(items))
	for i, item := range items {
		kind, value, _, err := rlp.Split(item)
		if err != nil {
			return common.Hash{}, err
		}
		if typed && kind == rlp.String {
			if len(value) < 2 || value[0] == 0 || value[0] >= 0x80 {
				return common.Hash{}, errors.New("evmdata: invalid typed item")
			}
			values[i] = value
		} else if kind == rlp.List {
			values[i] = item
		} else {
			return common.Hash{}, errors.New("evmdata: invalid consensus item")
		}
	}
	return types.DeriveSha(values, trie.NewStackTrie(nil)), nil
}

type Verifier struct {
	Profile    chains.BinaryProfile
	Provenance string
}

func (v Verifier) Assemble(ctx context.Context, raw *window.Store, state capture.State, refs []window.Reference) (capture.Input, error) {
	var empty capture.Input
	if v.Profile == nil || v.Provenance == "" || len(refs) != 3 {
		return empty, model.ErrCaptureIncomplete
	}
	if err := Capabilities(state.Anchor.Chain).CheckPlan(state.Anchor.Chain, state.Plan); err != nil {
		return empty, err
	}
	if len(state.Plan.Required) != 3 {
		return empty, model.ErrCaptureIncomplete
	}
	id := refs[0].Block
	data := map[model.DatasetKind][]byte{}
	for _, r := range refs {
		if r.Block != id || r.Codec != Codec(r.Kind) || r.PayloadBytes > 8<<20 || data[r.Kind] != nil {
			return empty, model.ErrCaptureInvalid
		}
		lease, err := raw.Acquire(ctx, r)
		if err != nil {
			return empty, err
		}
		e, err := lease.Envelope()
		if err == nil {
			data[r.Kind] = bytes.Clone(e.Payload)
		}
		err = errors.Join(err, lease.Close())
		if err != nil {
			return empty, err
		}
	}
	header, body, receipts := data[model.DatasetHeaders], data[model.DatasetBodies], data[model.DatasetReceipts]
	b, rs, err := Decode(v.Profile, id, header, body, receipts)
	if err != nil {
		return empty, err
	}
	digest, err := state.Plan.Digest()
	if err != nil {
		return empty, err
	}
	in := capture.Input{ParentHash: b.ParentHash, Manifest: model.CaptureManifest{Identity: model.CaptureIdentity{Block: id, Generation: state.Generation, PlanVersion: state.Plan.Version, PlanDigest: digest}}}
	for _, req := range state.Plan.Required {
		value, ok := data[req.Kind]
		if !ok {
			return empty, model.ErrCaptureIncomplete
		}
		in.Payloads = append(in.Payloads, capture.Payload{Kind: req.Kind, Bytes: value})
		in.Manifest.Datasets = append(in.Manifest.Datasets, model.DatasetEvidence{Kind: req.Kind, Coverage: req.Coverage, Status: model.DatasetPersisted, Digest: sha256.Sum256(value), Codec: Codec(req.Kind), Source: v.Provenance, Verification: model.VerificationCommitment})
	}
	if err := state.Plan.ValidateComplete(in.Manifest); err != nil {
		return empty, fmt.Errorf("evmdata: %w", err)
	}
	// Normalize only after all profile/commitment checks succeeded. The exact
	// encoded datasets remain the reconstruction source of truth.
	in.Rows, err = normalizedRows(b, rs)
	if err != nil {
		return empty, err
	}
	in.Normalized, err = capture.DescribeRows(in.Rows)
	if err != nil {
		return empty, err
	}
	return in, nil
}

func normalizedRows(b *model.Block, receipts []*model.Receipt) ([]capture.Row, error) {
	var rows []capture.Row
	total := 0
	add := func(k capture.RowKind, i uint32, v any) error {
		var value []byte
		var err error
		switch x := v.(type) {
		case *model.Block:
			value, err = model.EncodeBlock(x)
		case *model.Transaction:
			value, err = model.EncodeTransaction(x)
		case *model.Receipt:
			value, err = model.EncodeReceipt(x)
		default:
			value, err = json.Marshal(v)
		}
		if err != nil {
			return err
		}
		total += len(value)
		if len(value) > 8<<20 || total > 64<<20 {
			return model.ErrCaptureInvalid
		}
		rows = append(rows, capture.Row{Kind: k, Index: i, Value: value})
		return nil
	}
	header := *b
	header.Transactions, header.Uncles, header.Withdrawals = nil, nil, nil
	if err := add(capture.RowHeader, 0, &header); err != nil {
		return nil, err
	}
	body := capture.BodyFacts{Uncles: b.Uncles, Withdrawals: b.Withdrawals}
	for _, tx := range b.Transactions {
		body.TransactionHashes = append(body.TransactionHashes, tx.Hash)
	}
	if err := add(capture.RowBody, 0, body); err != nil {
		return nil, err
	}
	for i, tx := range b.Transactions {
		if err := add(capture.RowTransaction, uint32(i), tx); err != nil {
			return nil, err
		}
	}
	for i, r := range receipts {
		receipt := *r
		receipt.Logs = nil
		if err := add(capture.RowReceipt, uint32(i), &receipt); err != nil {
			return nil, err
		}
	}
	var index uint32
	for _, r := range receipts {
		for _, log := range r.Logs {
			if err := add(capture.RowLog, index, log); err != nil {
				return nil, err
			}
			index++
		}
	}
	return rows, nil
}

// Decode revalidates bounded encoded facts through their chain profile and
// header commitments. Both window capture and DB replay use this exact path.
func Decode(profile chains.BinaryProfile, id model.FeedBlock, header, body, receipts []byte) (*model.Block, []*model.Receipt, error) {
	if profile == nil || id.Validate() != nil {
		return nil, nil, model.ErrCaptureInvalid
	}
	if len(header) == 0 || len(header) > 64<<10 || len(body) == 0 || len(body) > 8<<20 || len(receipts) == 0 || len(receipts) > 8<<20 {
		return nil, nil, model.ErrCaptureIncomplete
	}
	parts, err := Items(body, 3)
	if err != nil || len(parts) < 2 {
		return nil, nil, errors.New("evmdata: invalid body")
	}
	if _, err := Items(parts[0], MaxItems); err != nil {
		return nil, nil, err
	}
	if _, err := Items(parts[1], 2); err != nil {
		return nil, nil, err
	}
	if len(parts) == 3 {
		if _, err := Items(parts[2], MaxItems); err != nil {
			return nil, nil, err
		}
	}
	ri, err := Items(receipts, MaxItems)
	if err != nil {
		return nil, nil, err
	}
	// Bound receipt/log/topic object counts before DeriveReceipts allocates.
	logCount := 0
	for _, item := range ri {
		kind, value, _, err := rlp.Split(item)
		if err != nil {
			return nil, nil, err
		}
		enc := []byte(item)
		if kind == rlp.String {
			if len(value) < 2 {
				return nil, nil, model.ErrCaptureInvalid
			}
			enc = value[1:]
		}
		fields, err := Items(enc, 4)
		if err != nil || len(fields) != 4 {
			return nil, nil, model.ErrCaptureInvalid
		}
		logs, err := Items(fields[3], MaxItems)
		if err != nil {
			return nil, nil, err
		}
		logCount += len(logs)
		if logCount > MaxItems {
			return nil, nil, model.ErrCaptureInvalid
		}
		for _, log := range logs {
			fields, err := Items(log, 3)
			if err != nil || len(fields) != 3 {
				return nil, nil, model.ErrCaptureInvalid
			}
			if _, err := Items(fields[1], 4); err != nil {
				return nil, nil, err
			}
		}
	}
	b, err := profile.DecodeBlockRLP(header, body)
	if err != nil {
		return nil, nil, err
	}
	if b == nil || b.Number != id.Number || b.Hash != id.Hash {
		return nil, nil, model.ErrCaptureInvalid
	}
	if len(ri) != len(b.Transactions) {
		return nil, nil, model.ErrCaptureIncomplete
	}
	for i, item := range ri {
		kind, value, _, err := rlp.Split(item)
		if err != nil {
			return nil, nil, err
		}
		receiptType := byte(0)
		if kind == rlp.String && len(value) > 0 {
			receiptType = value[0]
		}
		if receiptType != b.Transactions[i].Type {
			return nil, nil, errors.New("evmdata: receipt type mismatch")
		}
	}
	txRoot, err := Root(parts[0], true)
	if err != nil || txRoot != b.TxRoot || crypto.Keccak256Hash(parts[1]) != b.UncleHash {
		return nil, nil, errors.New("evmdata: body commitment mismatch")
	}
	if len(parts) == 3 {
		r, err := Root(parts[2], false)
		if err != nil || b.WithdrawalsRoot == nil || r != *b.WithdrawalsRoot {
			return nil, nil, errors.New("evmdata: withdrawals commitment mismatch")
		}
	} else if b.WithdrawalsRoot != nil {
		return nil, nil, errors.New("evmdata: missing withdrawals")
	}
	rr, err := Root(receipts, true)
	if err != nil || rr != b.ReceiptRoot {
		return nil, nil, errors.New("evmdata: receipt commitment mismatch")
	}
	rs, err := profile.DeriveReceipts(b, receipts)
	if err != nil {
		return nil, nil, err
	}
	if len(rs) != len(b.Transactions) {
		return nil, nil, model.ErrCaptureIncomplete
	}
	var gas uint64
	var bloom types.Bloom
	for _, r := range rs {
		if r == nil {
			return nil, nil, model.ErrCaptureIncomplete
		}
		if r.CumulativeGasUsed < gas {
			return nil, nil, errors.New("evmdata: decreasing receipt gas")
		}
		gas = r.CumulativeGasUsed
		if len(r.Bloom) != len(bloom) {
			return nil, nil, model.ErrCaptureInvalid
		}
		for i, x := range r.Bloom {
			bloom[i] |= x
		}
	}
	if gas != b.GasUsed || !bytes.Equal(bloom[:], b.Bloom) {
		return nil, nil, errors.New("evmdata: gas/bloom mismatch")
	}
	return b, rs, nil
}
