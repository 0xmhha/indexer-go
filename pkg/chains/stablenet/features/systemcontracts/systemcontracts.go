// Package systemcontracts is the stablenet.system_contracts feature: it
// indexes events of StableNet's system contracts (mint, burn, governance,
// blacklist and others) and publishes validator set changes.
package systemcontracts

import (
	"context"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/0xmhha/indexer-go/pkg/events"
	"github.com/0xmhha/indexer-go/pkg/feature"
	storagepkg "github.com/0xmhha/indexer-go/pkg/storage"
)

// Name is the feature name.
const Name = "stablenet.system_contracts"

type systemContractsFeature struct{}

func (systemContractsFeature) Name() string       { return Name }
func (systemContractsFeature) Requires() []string { return nil }

func (systemContractsFeature) Register(r feature.Registrar) error {
	d := r.Deps()
	w, ok := d.Storage.(storagepkg.SystemContractWriter)
	if !ok {
		return fmt.Errorf("storage does not support system contract events")
	}
	logger := d.Logger
	if logger == nil {
		logger = zap.NewNop()
	}
	r.OnBlock(&handler{
		parser:    events.NewSystemContractEventParser(w, logger),
		logger:    logger,
		publishFn: d.Publish,
	})
	return nil
}

func init() { feature.Register(systemContractsFeature{}) }

type handler struct {
	parser    *events.SystemContractEventParser
	logger    *zap.Logger
	publishFn func(events.Event) bool
}

// HandleBlock indexes the system contract events of every receipt and
// publishes validator set changes. A log that cannot be decoded is logged
// and skipped; a failure to store an event fails the block, so it is not
// committed without the event.
func (h *handler) HandleBlock(ctx context.Context, b *feature.Block) error {
	for _, receipt := range b.GethReceipts {
		if len(receipt.Logs) == 0 {
			continue
		}
		if err := h.parser.ParseAndIndexLogs(ctx, receipt.Logs); err != nil {
			return fmt.Errorf("index system contract events of %s: %w", receipt.TxHash.Hex(), err)
		}
		for _, l := range receipt.Logs {
			h.publishValidatorChange(b, l.Address, l.Topics)
		}
	}
	return nil
}

// publishValidatorChange publishes MemberAdded and MemberRemoved events of the
// GovValidator contract. The block hash comes from the model (D16).
func (h *handler) publishValidatorChange(b *feature.Block, addr common.Address, topics []common.Hash) {
	if h.publishFn == nil || addr != events.GovValidatorAddress || len(topics) < 2 {
		return
	}
	var change string
	switch topics[0] {
	case events.EventSigMemberAdded:
		change = "added"
	case events.EventSigMemberRemoved:
		change = "removed"
	default:
		return
	}
	validator := common.BytesToAddress(topics[1].Bytes())
	ev := events.NewValidatorSetEvent(b.Model.Number, b.Model.Hash, change, validator, "", 0)
	if !h.publishFn(ev) {
		h.logger.Warn("Failed to publish validator set event (channel full)",
			zap.String("type", change),
			zap.String("validator", validator.Hex()),
			zap.Uint64("block", b.Model.Number),
		)
		return
	}
	h.logger.Info("Validator "+change,
		zap.String("validator", validator.Hex()),
		zap.Uint64("block", b.Model.Number),
	)
}
