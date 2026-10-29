package config

import (
	"bytes"
	"encoding/hex"
	"fmt"
	stateproof "github.com/0xmhha/indexer-go/pkg/chainfeed/evmstate/proof"
	"github.com/0xmhha/indexer-go/pkg/chains"
	"github.com/0xmhha/indexer-go/pkg/execution/ingress"
	"math"
	"path/filepath"
	"strings"
	"time"
)

// ExecutionSourceConfig opts into a prepared chain-native execution database.
// Its checkpoint is the original anchor, not the changing visible height.
type ExecutionSourceConfig struct {
	Ingress             *ingress.Config         `yaml:"ingress,omitempty"`
	Consensus           *chains.ConsensusPolicy `yaml:"consensus"`
	RequiredStateProofs *stateproof.Selection   `yaml:"required_state_proofs"`
	RequireBlobSidecars bool                    `yaml:"require_blob_sidecars"`
	Socket              string                  `yaml:"socket"`
	AnchorHeight        uint64                  `yaml:"anchor_height"`
	AnchorHash          string                  `yaml:"anchor_hash"`
	AnchorRoot          string                  `yaml:"anchor_root"`
	QueueBlocks         int                     `yaml:"queue_blocks"`
	QueueBytes          int64                   `yaml:"queue_bytes"`
	Timeout             time.Duration           `yaml:"timeout"`
}

func (c *Config) validateExecution() error {
	e := c.Source.Execution
	if e == nil {
		return nil
	}
	if e.RequiredStateProofs != nil {
		if _, _, err := e.RequiredStateProofs.Normalize(); err != nil {
			return fmt.Errorf("source.execution.required_state_proofs: %w", err)
		}
	}
	if !filepath.IsAbs(e.Socket) {
		return fmt.Errorf("source.execution.socket must be an absolute private Unix socket path")
	}
	if c.Database.Driver != DriverPostgres || c.Database.Postgres.MaxConns != 4 {
		return fmt.Errorf("source.execution requires PostgreSQL max_conns=4")
	}
	if c.RPC.RateLimit != 5 {
		return fmt.Errorf("source.execution requires shared rpc.rate_limit=5")
	}
	if !strings.HasPrefix(c.RPC.Endpoint, "http://") && !strings.HasPrefix(c.RPC.Endpoint, "https://") || c.RPC.RecordDir != "" {
		return fmt.Errorf("source.execution requires live HTTP(S) canonical RPC without replay or recording")
	}
	if !validExecutionHash(e.AnchorHash) || !validExecutionHash(e.AnchorRoot) || e.AnchorHeight >= math.MaxInt64 {
		return fmt.Errorf("source.execution requires the explicit original anchor height/hash/root")
	}
	if e.QueueBlocks < 0 || e.QueueBlocks > 65536 || e.QueueBytes < 0 || e.QueueBytes > 2<<30 || e.Timeout < 0 || e.Timeout > time.Hour {
		return fmt.Errorf("source.execution queue or timeout exceeds supported bounds")
	}
	if c.MultiChain.Enabled || c.NodeRole() == RoleAPI || c.DeclaredMode() || c.Source.Feed != nil || c.Source.NodeFeedSocket != "" || c.Source.EraDir != "" {
		return fmt.Errorf("source.execution requires exclusive single-chain full ingest without another block source")
	}
	if !c.Indexer.StatePrefetch {
		return fmt.Errorf("source.execution requires indexer.state_prefetch")
	}
	if c.Indexer.Finality != "" && c.Indexer.Finality != "head" || c.Indexer.Confirmations != 0 {
		return fmt.Errorf("source.execution supports head canonical publication; finality/confirmations must not be silently ignored")
	}
	if e.Ingress != nil {
		if err := e.Ingress.Validate(); err != nil {
			return fmt.Errorf("source.execution.ingress: %w", err)
		}
		if c.Indexer.StartHeight != e.AnchorHeight+1 {
			return fmt.Errorf("source.execution.ingress start_height must equal the original anchor height + 1")
		}
	}
	return nil
}
func validExecutionHash(value string) bool {
	if !strings.HasPrefix(value, "0x") || len(value) != 66 {
		return false
	}
	hash, err := hex.DecodeString(value[2:])
	return err == nil && !bytes.Equal(hash, make([]byte, 32))
}
