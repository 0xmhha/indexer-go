package preflight

import (
	"path/filepath"

	"github.com/0xmhha/indexer-go/internal/config"
	"github.com/0xmhha/indexer-go/internal/nativepreflight"
	"github.com/0xmhha/indexer-go/pkg/core/model"
	"github.com/0xmhha/indexer-go/x/arc/ingress"
	"github.com/ethereum/go-ethereum/common"
)

// This hook touches detached declarations only; it opens no referenced path.
func managedApp(c *config.Config, unknown nativepreflight.Unknown, n *nativepreflight.Native, r *nativepreflight.Report) {
	e := c.Source.Execution
	if e == nil || e.Ingress == nil {
		return
	}
	i := e.Ingress
	if i.Module != arcingress.ModuleName {
		r.Add("app.ingress.module", nativepreflight.Fail, "unsupported_ingress_module")
		return
	}
	r.Add("app.ingress.live", nativepreflight.Hold, "managed_runtime_evidence_not_checked")
	if !n.Unknown["chain_id"] && n.Chain.ID != "5042002" {
		r.Add("app.ingress.chain", nativepreflight.Fail, "unsupported_managed_chain")
	}
	match := func(field, a, b string) {
		r.MatchKnown(field, a, b, nativepreflight.Unresolved(a) || nativepreflight.Unresolved(b))
	}
	match("app.ingress.native_data", i.Native.DataDir, n.Data)
	match("app.ingress.spool", i.Collector.SpoolDir, n.Spool)
	for _, path := range n.ExtraPaths {
		if path.Field == "native.executable" {
			match("app.ingress.replay_executable", i.Collector.Executable, path.Value)
		}
	}
	normal := i.Normalize()
	r.Match("app.ingress.spool_bytes", normal.Collector.MaxBytes, n.SpoolBytes)
	r.Match("app.ingress.spool_records", normal.Collector.MaxRecords, n.SpoolRecords)
	if i.InitializeVolume || i.CreateWindow {
		r.Add("app.ingress.prepared", nativepreflight.Fail, "prepared_storage_only")
	}
	if !nativepreflight.Unresolved(i.Collector.SpoolDir) && !nativepreflight.Unresolved(n.Scratch) {
		r.Match("app.ingress.scratch", filepath.Dir(i.Collector.SpoolDir), n.Scratch)
		n.SourceParentScratch = filepath.Dir(i.Collector.SpoolDir) == n.Scratch
	}
	n.ExtraPaths = append(n.ExtraPaths, nativepreflight.DeclaredPath{Field: "app.ingress.import_executable", Value: i.Native.Executable}, nativepreflight.DeclaredPath{Field: "app.ingress.peer_key", Value: i.Peer.KeyFile})
	neutral := func(field string, value *string, replacement string) {
		if nativepreflight.Unresolved(*value) {
			r.Add(field, nativepreflight.Hold, "operator_input_missing")
			*value = replacement
		}
	}
	neutral("app.ingress.native_data", &i.Native.DataDir, "/unresolved/native")
	neutral("app.ingress.import_executable", &i.Native.Executable, "/unresolved/importer")
	neutral("app.ingress.replay_executable", &i.Collector.Executable, "/unresolved/replay")
	neutral("app.ingress.spool", &i.Collector.SpoolDir, "/unresolved/source/spool")
	neutral("app.ingress.peer_key", &i.Peer.KeyFile, "/unresolved/peer.key")
	neutral("app.ingress.fork", &i.Peer.ForkHash, "01010101")
	n.NetworkID = i.Peer.NetworkID
	if unknown["ingress_network"] {
		i.Peer.NetworkID = 5042002
		n.Unknown["network_id"] = true
	} else if i.Peer.NetworkID != 5042002 {
		r.Add("app.ingress.network", nativepreflight.Fail, "known_identity_conflict")
	}
	var peers []string
	for _, peer := range i.Peer.Peers {
		if nativepreflight.Unresolved(peer) {
			n.Unknown["peers"] = true
		} else {
			peers = append(peers, peer)
		}
	}
	if len(i.Peer.Peers) > 8 {
		r.Add("app.ingress.peers", nativepreflight.Fail, "invalid_peer_limit")
	}
	n.Peers = peers
	if len(peers) == 0 || n.Unknown["peers"] {
		r.Add("app.ingress.peers", nativepreflight.Hold, "operator_input_missing")
	}
	if len(peers) == 0 {
		// Public generator point, detached validation only; never dialed.
		peers = []string{"enode://79be667ef9dcbbac55a06295ce870b07029bfcdb2dce28d959f2815b16f81798483ada7726a3c4655da4fbfc0e1108a8fd17b448a68554199c47d08ffb10d4b8@127.0.0.1:1"}
	}
	i.Peer.Peers = peers
	w, ok := c.WindowStorage.Windows[i.Window]
	if !ok {
		r.Add("app.ingress.raw", nativepreflight.Fail, "missing_raw_window")
		return
	}
	n.ExtraPaths = append(n.ExtraPaths, nativepreflight.DeclaredPath{Field: "app.ingress.raw", Value: w.Path})
	volume, ok := c.WindowStorage.Volumes[w.Volume]
	if !ok {
		r.Add("app.ingress.raw", nativepreflight.Fail, "missing_raw_volume")
		return
	}
	neutral("app.ingress.volume_root", &volume.Root, "/unresolved/raw-volume")
	neutral("app.ingress.volume_id", &volume.ID, "unresolved-raw")
	neutral("app.ingress.raw", &w.Path, filepath.Join(volume.Root, "raw"))
	c.WindowStorage.Volumes[w.Volume], c.WindowStorage.Windows[i.Window] = volume, w
	if err := i.Preflight(c.WindowStorage, e.Socket); err != nil {
		r.Add("app.ingress", nativepreflight.Fail, "invalid_ingress_layout_or_limits")
		return
	}
	o, err := c.WindowStorage.OptionsFor(i.Window, model.FeedChain{ID: n.Chain.ID, GenesisHash: common.Hash(n.Chain.Genesis)})
	if err != nil {
		r.Add("app.ingress.raw", nativepreflight.Fail, "invalid_raw_layout")
		return
	}
	n.RawBytes = uint64(o.MaxBytes)
	r.Add("app.ingress.raw", nativepreflight.Pass, "declared_raw_quota_only")
}
