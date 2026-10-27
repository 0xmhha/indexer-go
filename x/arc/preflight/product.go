// Package preflight is the Arc-only offline file adapter.
package preflight

import (
	"fmt"
	"github.com/0xmhha/indexer-go/execwire"

	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	"github.com/0xmhha/indexer-go/x/arc"
	"github.com/0xmhha/indexer-go/x/arc/collector"
	"github.com/0xmhha/indexer-go/x/arc/nativeops"
)

func Product() nativepreflight.Product {
	return nativepreflight.Product{Profile: "arc-native", SeparateRetention: true, CollectorScratch: true, ConsensusProfile: "arc", HeaderCodec: arc.New(), Decode: decode, Identity: func(c execwire.Chain, network uint64, u nativepreflight.Unknown, r *nativepreflight.Report) {
		if u["chain_id"] {
			return
		}
		if c.ID != "5042" && c.ID != "5042002" {
			r.Add("identity.chain_id", nativepreflight.Fail, "unsupported_chain")
			return
		}
		expected := uint64(5042)
		if c.ID == "5042002" {
			expected = 5042002
		}
		if !u["network"] && network != expected {
			r.Add("identity.network_id", nativepreflight.Fail, "known_identity_conflict")
		}
		if c.ID == "5042002" && !u["genesis"] {
			var h execwire.Hash
			_ = h.UnmarshalText([]byte("0xe20e653af4441e8c6088e172b129d56420139824400477287b46e7101ae2bb1f"))
			if c.Genesis != h {
				r.Add("identity.genesis", nativepreflight.Fail, "known_identity_conflict")
			}
		}
	}}

}
func decode(b, ret []byte, r *nativepreflight.Report) (nativepreflight.Native, error) {
	var c collector.Config
	var t nativeops.Config
	uf := identityFields("Native.Chain", "Anchor", "unused")
	uf = uf[:len(uf)-1]
	u, err := nativepreflight.DecodeDeclaredJSON(b, &c, "native", uf, r)
	if err != nil {
		return nativepreflight.Native{}, err
	}
	ut, err := nativepreflight.DecodeDeclaredJSON(ret, &t, "retention", identityFields("Chain", "Anchor", "AnchorRoot"), r)
	if err != nil {
		return nativepreflight.Native{}, err
	}
	// Missing paths are checked on neutral copies so known policy conflicts
	// still fail. Original observations remain unresolved in the report.
	validated := c
	for _, v := range []struct {
		field string
		value *string
	}{{"native.executable", &validated.Native.Executable}, {"native.data", &validated.Native.DataDir}, {"native.spool", &validated.SpoolDir}, {"native.socket", &validated.Socket}} {
		if nativepreflight.Unresolved(*v.value) {
			r.Add(v.field, nativepreflight.Hold, "operator_input_missing")
			*v.value = "/unresolved/" + v.field
		}
	}
	validatedRetention := t
	for _, v := range []struct {
		field string
		value *string
	}{{"retention.executable", &validatedRetention.NativeExecutable}, {"retention.data", &validatedRetention.NativeDataDir}, {"retention.spool", &validatedRetention.SpoolDir}} {
		if nativepreflight.Unresolved(*v.value) {
			r.Add(v.field, nativepreflight.Hold, "operator_input_missing")
			*v.value = "/unresolved/" + v.field
		}
	}
	// Backup/restore paths are optional as a group. Missing operator paths
	// inside a declared group stay unresolved; invalid known paths still fail.
	if t.BackupOutput != "" || t.RestoreManifest != "" || t.IndexerReceipt != "" || t.IndexerArtifact != "" {
		for _, v := range []struct {
			field string
			value *string
		}{{"indexer_receipt", &validatedRetention.IndexerReceipt}, {"indexer_artifact", &validatedRetention.IndexerArtifact}} {
			if nativepreflight.Unresolved(*v.value) {
				r.Add("retention."+v.field, nativepreflight.Hold, "operator_input_missing")
				*v.value = "/unresolved/" + v.field
			}
		}
		for _, v := range []struct {
			field string
			value *string
		}{{"backup_output", &validatedRetention.BackupOutput}, {"restore_manifest", &validatedRetention.RestoreManifest}} {
			if *v.value == "HOLD" {
				r.Add("retention."+v.field, nativepreflight.Hold, "operator_input_missing")
				*v.value = "/unresolved/" + v.field
			}
		}
		if validatedRetention.BackupOutput == "" && validatedRetention.RestoreManifest == "" {
			r.Add("retention.backup_output", nativepreflight.Hold, "operator_input_missing")
			validatedRetention.BackupOutput = "/unresolved/backup_output"
		}
	}
	if err := collector.ValidateExplicitConfig(validated); err != nil {
		r.Add("native.collector", nativepreflight.Fail, "production_validation_failed")
	}
	if err := nativeops.ValidateDeclared(validatedRetention); err != nil {
		r.Add("retention", nativepreflight.Fail, "production_validation_failed")
	}

	for _, m := range []struct {
		field string
		a, b  any
	}{{"retention.native_path", c.Native.DataDir, t.NativeDataDir}, {"retention.spool_path", c.SpoolDir, t.SpoolDir}, {"retention.spool_bytes", c.MaxBytes, t.SpoolBytes}, {"retention.spool_records", c.MaxRecords, t.SpoolRecords}} {
		r.Match(m.field, m.a, m.b)
	}
	r.ChainMatch("retention.chain", c.Native.Chain, t.Chain, u, ut)
	r.AnchorMatch("retention.anchor", c.Anchor, t.Anchor, u, ut)
	if t.SpoolRecords < 1 || t.SpoolBytes <= 64<<10 {
		r.Add("retention.limits", nativepreflight.Fail, "invalid_retention_limits")
	}
	if c.Anchor.Generation != 1 {
		r.Add("native.generation", nativepreflight.Fail, "invalid_original_generation")
	}
	if c.Native.ScratchDir == "" {
		r.Add("native.scratch", nativepreflight.Hold, "explicit_scratch_not_declared")
	}
	if c.Native.Chain.Genesis == (execwire.Hash{}) || c.Anchor.Hash == (execwire.Hash{}) || t.AnchorRoot == (execwire.Hash{}) {
		return nativepreflight.Native{}, fmt.Errorf("invalid identity")
	}
	// Preserve a known retention observation if the collector field is absent;
	// the collector's HOLD finding remains in the report.
	if u["chain_id"] && !ut["chain_id"] {
		c.Native.Chain.ID = t.Chain.ID
		u["chain_id"] = false
	}
	if u["genesis"] && !ut["genesis"] {
		c.Native.Chain.Genesis = t.Chain.Genesis
		u["genesis"] = false
	}
	if u["height"] && !ut["height"] {
		c.Anchor.Height = t.Anchor.Height
		u["height"] = false
	}
	if u["hash"] && !ut["hash"] {
		c.Anchor.Hash = t.Anchor.Hash
		u["hash"] = false
	}
	if u["generation"] && !ut["generation"] {
		c.Anchor.Generation = t.Anchor.Generation
		u["generation"] = false
	}
	if nativepreflight.Unresolved(c.Native.DataDir) && !nativepreflight.Unresolved(t.NativeDataDir) {
		c.Native.DataDir = t.NativeDataDir
	}
	if nativepreflight.Unresolved(c.SpoolDir) && !nativepreflight.Unresolved(t.SpoolDir) {
		c.SpoolDir = t.SpoolDir
	}
	u["root"] = ut["root"]
	extra := []nativepreflight.DeclaredPath{{Field: "native.executable", Value: c.Native.Executable}, {Field: "native.socket", Value: c.Socket}}
	// A declared native adapter may serve both commands. Check a
	// shared executable against all storage paths once, rather than against itself.
	if t.NativeExecutable != c.Native.Executable {
		extra = append(extra, nativepreflight.DeclaredPath{Field: "retention.executable", Value: t.NativeExecutable})
	}
	for _, p := range []nativepreflight.DeclaredPath{{Field: "retention.backup_output", Value: t.BackupOutput}, {Field: "retention.indexer_receipt", Value: t.IndexerReceipt}, {Field: "retention.indexer_artifact", Value: t.IndexerArtifact}, {Field: "retention.restore_manifest", Value: t.RestoreManifest}} {
		if p.Value != "" {
			extra = append(extra, p)
		}
	}
	return nativepreflight.Native{ExtraPaths: extra, Unknown: u, Chain: c.Native.Chain, Anchor: c.Anchor, Root: t.AnchorRoot, Data: c.Native.DataDir, Spool: c.SpoolDir, Scratch: c.Native.ScratchDir, Socket: c.Socket, SpoolBytes: c.MaxBytes, SpoolRecords: c.MaxRecords, Reserve: t.ReserveBytes, StateProofs: c.Native.StateProofs}, nil
}

func identityFields(chain, anchor, root string) []nativepreflight.Placeholder {
	fields := nativepreflight.IdentityFields(chain, anchor, root)
	fields[0].Neutral = "5042002"
	return fields
}
