package ante

import (
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmante "github.com/cosmos/evm/ante/evm"
	anteinterfaces "github.com/cosmos/evm/ante/interfaces"
)

// evmAccountKeeperWrapper adapts push-chain's AccountKeeper to satisfy the
// cosmos/evm interfaces.AccountKeeper, which requires unordered-tx methods not
// present in cosmos-sdk v0.50.x. Stubs return safe no-op values because
// unordered transactions are not enabled on this chain.
type evmAccountKeeperWrapper struct {
	AccountKeeper
}

var _ anteinterfaces.AccountKeeper = evmAccountKeeperWrapper{}

func (w evmAccountKeeperWrapper) UnorderedTransactionsEnabled() bool { return false }

func (w evmAccountKeeperWrapper) RemoveExpiredUnorderedNonces(_ sdk.Context) error { return nil }

func (w evmAccountKeeperWrapper) TryAddUnorderedNonce(_ sdk.Context, _ []byte, _ time.Time) error {
	return nil
}

// newMonoEVMAnteHandler creates the sdk.AnteHandler implementation for the EVM transactions.
func newMonoEVMAnteHandler(options HandlerOptions) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		evmante.NewEVMMonoDecorator(
			evmAccountKeeperWrapper{options.AccountKeeper},
			options.FeeMarketKeeper,
			options.EvmKeeper,
			options.MaxTxGasWanted,
		),
	)
}
