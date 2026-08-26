package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	cosmosevmcosmosante "github.com/cosmos/evm/ante/cosmos"
	evmante "github.com/cosmos/evm/ante/evm"
	evmtypes "github.com/cosmos/evm/x/vm/types"

	circuitante "cosmossdk.io/x/circuit/ante"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	ibcante "github.com/cosmos/ibc-go/v10/modules/core/ante"
	cosmosante "github.com/pushchain/push-chain-node/app/cosmos"
)

// NewCosmosAnteHandler creates the default ante handler for Cosmos transactions
func NewCosmosAnteHandler(ctx sdk.Context, options HandlerOptions) sdk.AnteHandler {
	feemarketParams := options.FeeMarketKeeper.GetParams(ctx)
	txFeeChecker := evmante.NewDynamicFeeChecker(&feemarketParams)

	return sdk.ChainAnteDecorators(
		cosmosevmcosmosante.NewRejectMessagesDecorator(), // reject MsgEthereumTxs
		cosmosevmcosmosante.NewAuthzLimiterDecorator( // disable the Msg types that cannot be included on an authz.MsgExec msgs field
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		),
		// Vesting accounts can delegate locked coins, but the EVM state view only
		// tracks spendable balance. Delegating more than the spendable balance makes
		// the StateDB subtract more than it holds, which reconciles back to bank as a
		// mint (or a burn for the victim). Block vesting-account creation outright so
		// the precondition cannot be created permissionlessly.
		NewBlockedMsgsDecorator(
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreatePermanentLockedAccount{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreatePeriodicVestingAccount{}),
		),

		ante.NewSetUpContextDecorator(),
		// Gasless txs pay no fee, so the fee is not a bound on the gas they
		// declare. Cap it explicitly, before NewGasWantedDecorator adds the
		// declared gas to the block's cumulative gas wanted.
		NewGaslessGasLimitDecorator(options.UexecutorKeeper),
		wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit), // after setup context to enforce limits early
		wasmkeeper.NewCountTXDecorator(options.TXCounterStoreService),
		wasmkeeper.NewGasRegisterDecorator(options.WasmKeeper.GetGasRegister()),
		circuitante.NewCircuitBreakerDecorator(options.CircuitKeeper),
		ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker),
		ante.NewValidateBasicDecorator(),
		ante.NewTxTimeoutHeightDecorator(),
		ante.NewValidateMemoDecorator(options.AccountKeeper),
		cosmosante.NewMinGasPriceDecorator(options.FeeMarketKeeper, options.EvmKeeper),
		ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper),
		NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, txFeeChecker),
		ibcante.NewRedundantRelayDecorator(options.IBCKeeper),
		evmante.NewGasWantedDecorator(options.EvmKeeper, options.FeeMarketKeeper, &feemarketParams),
		// NewAccountInitDecorator must be called before all signature verification decorators and SetPubKeyDecorator
		// - this
		// 1. generates the account for the new accounts only for gasless transactions,
		// 2. binds the declared signer to the signing key, enforces the signature
		//    count limit and verifies the sig, and
		// 3. bypasses the rest of the ante chain
		NewAccountInitDecorator(options.AccountKeeper, options.SignModeHandler),
		// SetPubKeyDecorator must be called before all signature verification decorators
		ante.NewSetPubKeyDecorator(options.AccountKeeper),
		ante.NewValidateSigCountDecorator(options.AccountKeeper),
		ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer),
		ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler),
		ante.NewIncrementSequenceDecorator(options.AccountKeeper),
	)
}
