package nodebackend

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/retention"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/nodestore"
	"github.com/0xmhha/indexer-go/x/stablenet/executor/spool"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/ethereum/go-ethereum/triedb/pathdb"
)

type retentionVector struct {
	Version        int
	Scope, Profile string
	Request        retention.Request
	Genesis        []byte
	Payloads       [][]byte
}

func producerFixture(t *testing.T) (Config, retentionVector) {
	t.Helper()
	c, g := runtimeConfig(t)
	c.Peers = nil
	c.SpoolBytes = 8 << 20
	c.SpoolRecords = 64
	c.RetentionReserveBytes = 1
	g.Alloc = types.GenesisAlloc{common.Address{19: 42}: {Balance: big.NewInt(123)}}
	genesis := g.ToBlock()
	c.GenesisHash, c.AnchorHash, c.AnchorRoot = genesis.Hash().Hex(), genesis.Hash().Hex(), genesis.Root().Hex()
	data, e := json.Marshal(g)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(c.GenesisFile, data, 0600); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(c.DataDir, "indexer-stablenet", "chaindata")
	if e = os.MkdirAll(path, 0700); e != nil {
		t.Fatal(e)
	}
	disk, e := rawdb.Open(rawdb.OpenOptions{Directory: path, AncientsDirectory: filepath.Join(path, "ancient"), Cache: 16, Handles: 32})
	if e != nil {
		t.Fatal(e)
	}
	database := state.NewDatabaseWithConfig(disk, &triedb.Config{PathDB: pathdb.Defaults, Preimages: true})
	genesis, e = g.Commit(disk, database.TrieDB())
	if e != nil {
		t.Fatal(e)
	}
	chain := execwire.Chain{ID: c.ChainID, Genesis: execwire.Hash(genesis.Hash())}
	anchor := execwire.Ref{Generation: 1, Hash: chain.Genesis}
	source, e := spool.Open(t.Context(), spool.Options{Path: c.SpoolDir, Chain: chain, Anchor: anchor, MaxBytes: c.SpoolBytes, MaxRecords: c.SpoolRecords})
	if e != nil {
		t.Fatal(e)
	}
	batch := disk.NewBatch()
	parent := genesis
	vector := retentionVector{Version: 1, Scope: "synthetic storage/protocol fixture, not consensus-valid peer data", Profile: "stablenet"}
	for n := uint64(1); n <= 132; n++ {
		h := &types.Header{ParentHash: parent.Hash(), Number: new(big.Int).SetUint64(n), Root: genesis.Root(), Time: n, GasLimit: 30000000, Difficulty: big.NewInt(1), TxHash: types.EmptyTxsHash, ReceiptHash: types.EmptyReceiptsHash, UncleHash: types.EmptyUncleHash}
		b := types.NewBlockWithHeader(h)
		rawdb.WriteBlock(batch, b)
		rawdb.WriteCanonicalHash(batch, b.Hash(), n)
		rawdb.WriteTd(batch, b.Hash(), n, big.NewInt(int64(n+1)))
		rawdb.WriteReceipts(batch, b.Hash(), n, nil)
		blockRLP, e := rlp.EncodeToBytes(b)
		if e != nil {
			t.Fatal(e)
		}
		result := execwire.Result{Ref: execwire.Ref{Generation: 2, Height: n, Hash: execwire.Hash(b.Hash())}, Parent: execwire.Hash(parent.Hash()), Time: n, StateRoot: execwire.Hash(b.Root()), ReceiptsRoot: execwire.Hash(b.ReceiptHash()), BlockRLP: blockRLP, ReceiptsRLP: []byte{0xc0}, Steps: []execwire.Step{{Ordinal: 0, Cause: execwire.Finalize, TxIndex: -1}}}
		payload, e := execwire.MarshalResult(result)
		if e != nil {
			t.Fatal(e)
		}
		vector.Payloads = append(vector.Payloads, payload)
		if n <= 4 {
			result.Ref.Generation = 1
			if _, e = source.Append(t.Context(), result); e != nil {
				t.Fatal(e)
			}
		}
		parent = b
	}
	rawdb.WriteHeadBlockHash(batch, parent.Hash())
	rawdb.WriteHeadHeaderHash(batch, parent.Hash())
	rawdb.WriteHeadFastBlockHash(batch, parent.Hash())
	if e = batch.Write(); e != nil {
		t.Fatal(e)
	}
	if e = errors.Join(database.TrieDB().Close(), disk.Close()); e != nil {
		t.Fatal(e)
	}
	base := anchor
	base.Generation = 2
	if e = source.Rebind(t.Context(), base); e != nil {
		t.Fatal(e)
	}
	for _, payload := range vector.Payloads {
		r, e := execwire.UnmarshalResult(payload)
		if e != nil {
			t.Fatal(e)
		}
		entry, e := source.Append(t.Context(), r)
		if e != nil {
			t.Fatal(e)
		}
		if e = source.Acknowledge(t.Context(), execwire.Frame{Kind: execwire.Committed, Header: execwire.Header{Version: execwire.Version, Chain: chain, Ref: entry.Ref, Digest: entry.Digest}}); e != nil {
			t.Fatal(e)
		}
	}
	if e = source.Close(); e != nil {
		t.Fatal(e)
	}
	var exported bytes.Buffer
	if e = ExportGenesis(t.Context(), c, &exported); e != nil {
		t.Fatal(e)
	}
	vector.Genesis = exported.Bytes()
	vector.Request, e = PlanRetention(t.Context(), c, 129)
	if e != nil {
		t.Fatal(e)
	}
	return c, vector
}
func TestRetentionFrozenProducerToolRoundTrip(t *testing.T) {
	c, vector := producerFixture(t)
	encoded, e := json.MarshalIndent(vector, "", "  ")
	if e != nil {
		t.Fatal(e)
	}
	encoded = append(encoded, '\n')
	path := "../../../../testdata/execution/retention/stablenet-storage.json"
	if os.Getenv("INDEXER_UPDATE_EXECUTION_FIXTURES") == "1" {
		if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(path, encoded, 0644); e != nil {
			t.Fatal(e)
		}
	}
	frozen, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(encoded, frozen) {
		t.Fatal("producer artifact drift; inspect before explicit regeneration")
	}
	dir := t.TempDir()
	if e = os.Chmod(dir, 0700); e != nil {
		t.Fatal(e)
	}
	config := filepath.Join(dir, "executor.json")
	data, e := json.Marshal(c)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(config, data, 0600); e != nil {
		t.Fatal(e)
	}
	var output bytes.Buffer
	planPath := filepath.Join(dir, "request.json")
	if e = RunRetention(t.Context(), RetentionCLI{Action: "plan", Config: config, Keep: 129, Output: planPath}, &output); e != nil {
		t.Fatal(e)
	}
	var request retention.Request
	if e = retention.ReadArtifact(planPath, &request); e != nil || request != vector.Request {
		t.Fatal("operator plan diverged", e)
	}
	p := retention.Permit{Version: 1, Coverage: "full", BaselineDigest: execwire.Hash(sha256.Sum256(vector.Genesis)), Token: execwire.Hash{77}, Request: request, Visible: request.ACK, Root: request.HeadRoot}
	permitPath := filepath.Join(dir, "permit.json")
	if e = retention.WriteArtifact(permitPath, p); e != nil {
		t.Fatal(e)
	}
	receiptPath := filepath.Join(dir, "receipt.json")
	if e = RunRetention(t.Context(), RetentionCLI{Action: "apply", Config: config, Permit: permitPath}, &output); e == nil {
		t.Fatal("stale artifact bypassed live DB supervisor")
	}
	supervisorRead, supervisorWrite, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	pd, e := retention.Digest(p)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.NewEncoder(supervisorWrite).Encode(retention.Supervision{Version: 1, PermitDigest: pd, Challenge: execwire.Hash{44}}); e != nil {
		t.Fatal(e)
	}
	if e = RunRetention(t.Context(), RetentionCLI{Action: "apply", Config: config, Permit: permitPath, Output: receiptPath, Supervisor: supervisorRead}, &output); e != nil {
		t.Fatal(e)
	}
	_ = supervisorWrite.Close()
	var reply retention.SupervisedReply
	if e = retention.ReadArtifact(receiptPath, &reply); e != nil {
		t.Fatal(e)
	}
	receipt := reply.Receipt
	if e = receipt.Match(p); e != nil || receipt.Match(p) != nil || receipt.Files != 3 {
		t.Fatal("bad producer receipt", receipt, e)
	}
	if e = RunRetention(t.Context(), RetentionCLI{Action: "inspect", Config: config}, &output); e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(output.Bytes(), []byte(`"MaintenancePending":true`)) {
		t.Fatal("inspection hid pinned GC")
	}
	rd, e := retention.Digest(receipt)
	if e != nil {
		t.Fatal(e)
	}
	pd, e = retention.Digest(p)
	if e != nil {
		t.Fatal(e)
	}
	releasePath := filepath.Join(dir, "release.json")
	if e = retention.WriteArtifact(releasePath, retention.Release{Version: 1, PermitDigest: pd, ReceiptDigest: rd}); e != nil {
		t.Fatal(e)
	}
	if e = RunRetention(t.Context(), RetentionCLI{Action: "release", Config: config, Permit: permitPath, Release: releasePath}, &output); e != nil {
		t.Fatal(e)
	}
	if e = nodestore.CheckStart(c.DataDir); e != nil {
		t.Fatal(e)
	}
	options, _, e := retentionOptions(c)
	if e != nil {
		t.Fatal(e)
	}
	s, e := spool.Open(t.Context(), options)
	if e != nil {
		t.Fatal(e)
	}
	status, e := s.Status(t.Context())
	if e != nil || status.Quarantined != 1 || status.Committed != request.ACK {
		t.Fatal("window/ACK changed", status, e)
	}
	if e = s.Close(); e != nil {
		t.Fatal(e)
	}
}
