package api

import (
	"encoding/json"
	"errors"
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"go.uber.org/zap"
	"net/http"
)

func init() {
	RegisterRoute(Route{Method: http.MethodGet, Pattern: "/execution/blobs/{kind}/{key}", Handler: executionBlobHandler})
}

type executionBlobJSON struct {
	Index            uint32        `json:"index"`
	TransactionIndex uint32        `json:"transactionIndex"`
	TransactionHash  common.Hash   `json:"transactionHash"`
	BlobIndex        uint32        `json:"blobIndex"`
	VersionedHash    common.Hash   `json:"versionedHash"`
	Blob             hexutil.Bytes `json:"blob"`
	Commitment       hexutil.Bytes `json:"commitment"`
	Proof            hexutil.Bytes `json:"proof"`
}
type executionBlobPageJSON struct {
	Epoch        uint64              `json:"epoch"`
	Ref          execwire.Ref        `json:"ref"`
	Digest       execwire.Hash       `json:"digest"`
	Format       string              `json:"format"`
	Profile      string              `json:"profile"`
	Provider     string              `json:"provider"`
	Availability string              `json:"availability"`
	Verification string              `json:"verification"`
	Total        uint32              `json:"total"`
	Entries      []executionBlobJSON `json:"entries"`
	Next         *uint32             `json:"next"`
}

func executionBlobHandler(store port.QueryStore, _ *zap.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		fail := func(status int, code string) {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
		}
		parsed, e := parseExecutionQuery(r, 4, 1, 32, false)
		if e != nil {
			fail(400, "invalid_execution_blob_query")
			return
		}
		reader, ok := store.(port.ExecutionBlobReader)
		if !ok {
			fail(503, "execution_blobs_unavailable")
			return
		}
		p, e := reader.ExecutionBlobs(r.Context(), port.ExecutionBlobQuery{Height: parsed.Height, Transaction: parsed.Transaction, After: parsed.After, Limit: parsed.Limit, Epoch: parsed.Epoch, Hash: parsed.Hash})
		if e != nil {
			switch {
			case errors.Is(e, port.ErrNotFound):
				fail(404, "execution_blobs_not_collected")
			case errors.Is(e, port.ErrInvalidData):
				fail(400, "invalid_execution_blob_query")
			case errors.Is(e, port.ErrExecutionStale):
				fail(409, "execution_blob_cursor_stale")
			case errors.Is(e, port.ErrExecutionUninitialized), errors.Is(e, port.ErrExecutionRewinding), errors.Is(e, port.ErrExecutionMaintenance):
				fail(503, "execution_blobs_unavailable")
			default:
				fail(500, "execution_blobs_error")
			}
			return
		}
		if len(p.Entries) != len(p.References) || len(p.Entries) > int(parsed.Limit) {
			fail(500, "execution_blobs_error")
			return
		}
		out := executionBlobPageJSON{Epoch: p.Epoch, Ref: p.Ref, Digest: p.Digest, Format: execwire.FullBlobFormat, Profile: p.Profile, Provider: p.Provider, Availability: p.Availability, Verification: p.Verification, Total: p.Total, Entries: make([]executionBlobJSON, 0, len(p.Entries)), Next: p.Next}
		for i, v := range p.Entries {
			ref := p.References[i]
			if v.Index != ref.Index || len(v.Blob) != 131072 || len(v.Commitment) != 48 || len(v.Proof) != 48 {
				fail(500, "execution_blobs_error")
				return
			}
			out.Entries = append(out.Entries, executionBlobJSON{Index: v.Index, TransactionIndex: ref.TransactionIndex, TransactionHash: common.Hash(ref.TransactionHash), BlobIndex: ref.BlobIndex, VersionedHash: common.Hash(ref.VersionedHash), Blob: hexutil.Bytes(v.Blob), Commitment: hexutil.Bytes(v.Commitment), Proof: hexutil.Bytes(v.Proof)})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}
