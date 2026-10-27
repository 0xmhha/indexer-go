// Package nativecfg owns the StableNet executor file schema without linking its EVM.
package nativecfg

import (
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"net"
	"path/filepath"
	"strings"
)

type Config struct {
	StartMode             string          `json:"start_mode"`
	DataDir               string          `json:"data_dir"`
	SpoolDir              string          `json:"spool_dir"`
	Socket                string          `json:"socket"`
	GenesisFile           string          `json:"genesis_file"`
	ChainID               string          `json:"chain_id"`
	NetworkID             uint64          `json:"network_id"`
	GenesisHash           string          `json:"genesis_hash"`
	AnchorHeight          uint64          `json:"anchor_height"`
	AnchorHash            string          `json:"anchor_hash"`
	AnchorRoot            string          `json:"anchor_root"`
	Peers                 []string        `json:"peers"`
	ListenAddr            string          `json:"listen_addr"`
	SpoolBytes            uint64          `json:"spool_bytes"`
	SpoolRecords          int             `json:"spool_records"`
	RetentionReserveBytes uint64          `json:"retention_reserve_bytes,omitempty"`
	Capacity              *CapacityConfig `json:"capacity,omitempty"`
}
type CapacityConfig struct {
	NodeBytes       uint64 `json:"node_bytes"`
	StopFreeBytes   uint64 `json:"stop_free_bytes"`
	ResumeFreeBytes uint64 `json:"resume_free_bytes"`
	IntervalSeconds uint64 `json:"interval_seconds"`
}

func (c *CapacityConfig) Validate() error {
	if c == nil {
		return nil
	}
	if c.NodeBytes == 0 || c.StopFreeBytes == 0 || c.ResumeFreeBytes <= c.StopFreeBytes || c.IntervalSeconds > 3600 {
		return fmt.Errorf("executor: capacity needs positive node_bytes, stop_free_bytes < resume_free_bytes and interval_seconds <= 3600")
	}
	return nil
}

// ValidateDeclared is the pure portion of production settings validation.
// Genesis contents and peer curve points are checked by the linked client.
func (c Config) ValidateDeclared() error {
	invalid := func() error { return fmt.Errorf("executor: invalid declared settings") }
	id, ok := new(big.Int).SetString(c.ChainID, 10)
	if !ok || id.Sign() <= 0 || id.String() != c.ChainID || c.Capacity.Validate() != nil || c.StartMode != "" && c.StartMode != "genesis" && c.StartMode != "checkpoint" || c.StartMode == "genesis" && c.AnchorHeight != 0 {
		return invalid()
	}
	if !filepath.IsAbs(c.DataDir) || !filepath.IsAbs(c.SpoolDir) || !filepath.IsAbs(c.Socket) || !filepath.IsAbs(c.GenesisFile) || c.NetworkID == 0 || c.AnchorHeight >= math.MaxInt64 || len(c.Peers) > 64 || c.SpoolRecords < 0 {
		return invalid()
	}
	for _, h := range []string{c.GenesisHash, c.AnchorHash, c.AnchorRoot} {
		if !ValidHash(h) {
			return invalid()
		}
	}

	a, b := filepath.Clean(c.DataDir), filepath.Clean(c.SpoolDir)
	if a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") {
		return invalid()
	}
	listen := c.ListenAddr
	if listen == "" {
		listen = "127.0.0.1:0"
	}
	if _, _, err := net.SplitHostPort(listen); err != nil {
		return invalid()
	}
	return nil
}

// ValidHash is shared with offline checkpoint inspection.
func ValidHash(value string) bool {
	if len(value) != 66 || !strings.HasPrefix(value, "0x") {
		return false
	}
	b, err := hex.DecodeString(value[2:])
	if err != nil {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}
