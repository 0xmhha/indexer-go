package chains

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/0xmhha/indexer-go/execwire"
)

// ConsensusPolicy is pinned before recovery. Parameters contain the complete
// chain-specific authenticated anchor and fork policy; no RPC fills omissions.
type ConsensusPolicy struct {
	Profile    string `yaml:"profile"`
	Protocol   string `yaml:"protocol"`
	Parameters string `yaml:"parameters"`
}
type ConsensusPoint struct {
	Height     uint64
	Hash, Root execwire.Hash
	State      json.RawMessage
}
type ConsensusVerifier interface {
	AnchorPoint() (ConsensusPoint, error)
	RestorePoint(context.Context, json.RawMessage) (ConsensusPoint, error)
	VerifyPoint(context.Context, json.RawMessage, []byte) (ConsensusPoint, error)
}
type ConsensusFactory func(execwire.Chain, ConsensusPolicy) (ConsensusVerifier, error)

var consensusFactories = struct {
	sync.RWMutex
	values map[string]ConsensusFactory
}{values: make(map[string]ConsensusFactory)}

func RegisterConsensus(protocol string, factory ConsensusFactory) {
	consensusFactories.Lock()
	defer consensusFactories.Unlock()
	if factory == nil || consensusFactories.values[protocol] != nil {
		panic("duplicate or nil consensus factory")
	}
	consensusFactories.values[protocol] = factory
}
func NewConsensusVerifier(chain execwire.Chain, policy ConsensusPolicy) (ConsensusVerifier, error) {
	consensusFactories.RLock()
	factory := consensusFactories.values[policy.Protocol]
	consensusFactories.RUnlock()
	if factory == nil {
		return nil, fmt.Errorf("unsupported independent consensus protocol %q", policy.Protocol)
	}
	return factory(chain, policy)
}
