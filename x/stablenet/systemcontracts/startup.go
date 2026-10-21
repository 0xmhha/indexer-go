package systemcontracts

import (
	"context"
	"errors"

	"github.com/0xmhha/indexer-go/pkg/core/port"
	"github.com/0xmhha/indexer-go/pkg/startup"
)

// StartupStep names the startup step that stores the system contracts'
// verification data.
const StartupStep = "stablenet.system_contract_verifications"

func init() { startup.Register(StartupStep, prepareVerifications) }

// prepareVerifications stores the verification data of the system
// contracts when system_contracts.enabled names a source path.
func prepareVerifications(ctx context.Context, env startup.Env) error {
	sc := env.Config.SystemContracts
	if !sc.Enabled || sc.SourcePath == "" {
		return nil
	}
	writer, ok := env.Storage.(port.ContractVerificationWriter)
	if !ok {
		return errors.New("storage does not support contract verification writes")
	}
	reader, ok := env.Storage.(port.ContractVerificationReader)
	if !ok {
		return errors.New("storage does not support contract verification reads")
	}
	return InitSystemContractVerifications(ctx, writer, reader, &SystemContractVerificationConfig{
		SourcePath:       sc.SourcePath,
		IncludeAbstracts: sc.IncludeAbstracts,
		Logger:           env.Logger,
	})
}
