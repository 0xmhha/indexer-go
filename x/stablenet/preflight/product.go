// Package preflight links only StableNet's declared schema and E6 verifier.
// The node executor's EVM dependency remains isolated in its own module.
package preflight

import (
	"github.com/0xmhha/indexer-go/execwire"
	"github.com/0xmhha/indexer-go/execwire/configfile"
	"github.com/0xmhha/indexer-go/execwire/nativecfg"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/x/stablenet"
	"github.com/0xmhha/indexer-go/x/stablenet/consensus"
)

func Product() nativepreflight.Product {
	return nativepreflight.Product{Profile: "stablenet-native", PeerMembership: true, ConsensusProfile: "stablenet", ConsensusProtocol: consensus.IndependentProtocol, HeaderCodec: stablenet.New(), Decode: decode, Consensus: func(c execwire.Chain, p chains.ConsensusPolicy) (chains.ConsensusVerifier, error) {
		// Check embedded policy duplicates before the production trust decoder.
		policy, err := consensus.DecodeTrustPolicy([]byte(p.Parameters))
		if err != nil {
			return nil, configfile.ErrInput
		}
		if c.ID != "" && c.ID != policy.Chain.ID || c.Genesis != (execwire.Hash{}) && c.Genesis != policy.Chain.Genesis {
			return nil, configfile.ErrInput
		}
		return chains.NewConsensusVerifier(policy.Chain, p)
	}}
}
func decode(b, _ []byte, r *nativepreflight.Report) (nativepreflight.Native, error) {
	var c nativecfg.Config
	u, err := nativepreflight.DecodeDeclaredJSON(b, &c, "native", []nativepreflight.Placeholder{{Path: "chain_id", Key: "chain_id", Neutral: "1"}, {Path: "genesis_hash", Key: "genesis", Neutral: nativepreflight.NeutralHash}, {Path: "anchor_height", Key: "height", Neutral: 0}, {Path: "anchor_hash", Key: "hash", Neutral: nativepreflight.NeutralHash}, {Path: "anchor_root", Key: "root", Neutral: nativepreflight.NeutralHash}, {Path: "network_id", Key: "network_id", Neutral: 1}}, r)
	if err != nil {
		return nativepreflight.Native{}, err
	}
	// Genesis mode itself declares height zero. Keep the missing-input HOLD,
	// but compare that known mode constraint against every other input.
	if c.StartMode == "genesis" && u["height"] {
		c.AnchorHeight = 0
		u["height"] = false
	}
	chain := execwire.Chain{ID: c.ChainID}
	var hash, root execwire.Hash
	if chain.Genesis.UnmarshalText([]byte(c.GenesisHash)) != nil || hash.UnmarshalText([]byte(c.AnchorHash)) != nil || root.UnmarshalText([]byte(c.AnchorRoot)) != nil || chain.Genesis == (execwire.Hash{}) || hash == (execwire.Hash{}) || root == (execwire.Hash{}) {
		return nativepreflight.Native{}, configfile.ErrInput
	}
	validated := c
	for _, p := range []struct {
		field string
		value *string
	}{{"data", &validated.DataDir}, {"spool", &validated.SpoolDir}, {"socket", &validated.Socket}, {"genesis_file", &validated.GenesisFile}} {
		if nativepreflight.Unresolved(*p.value) {
			r.Add("native."+p.field, nativepreflight.Hold, "operator_input_missing")
			*p.value = "/unresolved/" + p.field
		}
	}
	if err := validated.ValidateDeclared(); err != nil {
		r.Add("native.executor", nativepreflight.Fail, "production_validation_failed")
	}
	if c.SpoolBytes != 0 && c.SpoolBytes <= 64<<10 || c.SpoolRecords > 1<<20 {
		r.Add("native.quotas", nativepreflight.Fail, "invalid_spool_limits")
	}
	if c.SpoolBytes == 0 {
		c.SpoolBytes = 4 << 30
		r.Add("native.spool_bytes", nativepreflight.Pass, "production_default_quota")
	}
	if c.SpoolRecords == 0 {
		c.SpoolRecords = 262144
		r.Add("native.spool_records", nativepreflight.Pass, "production_default_quota")
	}

	r.Path("native.genesis_file", c.GenesisFile)
	if len(c.Peers) == 0 {
		r.Add("native.peers", nativepreflight.Hold, "operator_input_missing")
	}
	for _, peer := range c.Peers {
		if nativepreflight.Unresolved(peer) {
			r.Add("native.peers", nativepreflight.Hold, "operator_input_missing")
		} else if !nativepreflight.ValidEnode(peer) {
			r.Add("native.peers", nativepreflight.Fail, "invalid_enode")
		}
	}
	return nativepreflight.Native{Unknown: u, ExtraPaths: []nativepreflight.DeclaredPath{{Field: "native.genesis_file", Value: c.GenesisFile}, {Field: "native.socket", Value: c.Socket}}, Chain: chain, Anchor: execwire.Ref{Generation: 1, Height: c.AnchorHeight, Hash: hash}, Root: root, Data: c.DataDir, Spool: c.SpoolDir, Socket: c.Socket, GenesisFile: c.GenesisFile, SpoolBytes: c.SpoolBytes, SpoolRecords: c.SpoolRecords, Reserve: c.RetentionReserveBytes, NetworkID: c.NetworkID, Peers: c.Peers, StateProofs: (*stateproof.Selection)(nil)}, nil
}
