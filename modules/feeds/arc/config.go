package arcfeed

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/0xmhha/indexer-go/pkg/capture"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/ethpeer"
	"github.com/0xmhha/indexer-go/pkg/chainfeed/evmdata"
	"github.com/0xmhha/indexer-go/pkg/chains/arc"
	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/feed"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/enode"
)

const ModuleName = "arc.eth68"

func init() {
	blobs := arc.BlobCapability()
	feed.Register(ModuleName, feed.Module{BlobCapability: &blobs, CallTraceProfile: "arc", SupportsStateProofs: true, Validate: validateConfig, Open: openConfig})
}
func validateConfig(c feed.Config) error {
	if c.ChainID != "5042002" {
		return fmt.Errorf("arc.eth68: only audited Arc testnet 5042002 is supported")
	}
	if c.ListenAddr != "" {
		host, port, err := net.SplitHostPort(c.ListenAddr)
		n, e := strconv.Atoi(port)
		if err != nil || e != nil || net.ParseIP(host) == nil || n < 0 || n > 65535 {
			return fmt.Errorf("arc.eth68: listen_addr requires a literal IP and valid TCP port")
		}
	}
	seen := map[enode.ID]bool{}
	for _, url := range c.Peers {
		n, err := enode.ParseV4(url)
		if err != nil || (n.IP() == nil && n.Hostname() == "") || n.TCP() == 0 || seen[n.ID()] {
			return fmt.Errorf("arc.eth68: invalid or duplicate static enode")
		}
		seen[n.ID()] = true
	}
	if c.FinalityAuthority != "" {
		return validateAuthority(c.FinalityAuthority)
	}
	return nil
}

func openConfig(ctx context.Context, c feed.Config, fresh feed.Fresh) (feed.Program, error) {
	anchor, err := c.Identity()
	if err != nil {
		return feed.Program{}, err
	}
	// A key is supplied explicitly, never generated in an unexpected directory.
	file, err := os.Open(c.KeyFile)
	if err != nil {
		return feed.Program{}, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return feed.Program{}, err
	}
	pathInfo, e := os.Lstat(c.KeyFile)
	if e != nil || !os.SameFile(info, pathInfo) || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 128 {
		return feed.Program{}, fmt.Errorf("arc.eth68: key_file must be a private regular file (0600)")
	}
	encoded, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil {
		return feed.Program{}, err
	}
	key, err := crypto.HexToECDSA(strings.TrimSpace(string(encoded)))
	if err != nil {
		return feed.Program{}, fmt.Errorf("arc.eth68: cannot load peer identity key")
	}

	fork, _ := hex.DecodeString(c.ForkHash)
	peer := ethpeer.Config{Chain: anchor.Chain, Checkpoint: anchor, NetworkID: c.NetworkID, Key: key, ListenAddr: c.ListenAddr, MaxPeers: len(c.Peers)}
	copy(peer.ForkID.Hash[:], fork)
	peer.ForkID.Next = c.ForkNext
	for _, url := range c.Peers {
		n, _ := enode.ParseV4(url)
		peer.Peers = append(peer.Peers, n)
	}
	for _, b := range []struct {
		n    uint64
		hash [32]byte
	}{{0, anchor.Chain.GenesisHash}, {anchor.Number, anchor.Hash}} {
		h, e := fresh.HashAt(ctx, b.n)
		if e != nil {
			return feed.Program{}, e
		}
		if h != b.hash {
			return feed.Program{}, capture.ErrCanonical
		}
	}
	p := feed.Program{Chain: anchor.Chain, Anchor: anchor, Plan: evmdata.BasePlan(), Profile: arc.New()}
	if c.FinalityAuthority != "" {
		p.Policy, err = TrustedRPCFinality(fresh, anchor.Chain, c.FinalityAuthority)
		if err != nil {
			return p, err
		}
	}
	p.Run = func(ctx context.Context, db port.CaptureDatabase, session capture.SessionConfig) error {
		return RunManaged(ctx, db, fresh, peer, session, capture.RuntimeOptions{Collect: c.Collect, ControlInterval: c.ControlInterval}, c.FinalityAuthority)
	}
	return p, nil
}
