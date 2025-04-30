package ante

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	sdkvesting "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	evmoscosmosante "github.com/evmos/os/ante/cosmos"
	evmante "github.com/evmos/os/ante/evm"
	evmtypes "github.com/evmos/os/x/evm/types"

	circuitante "cosmossdk.io/x/circuit/ante"
	wasmkeeper "github.com/CosmWasm/wasmd/x/wasm/keeper"
	ibcante "github.com/cosmos/ibc-go/v8/modules/core/ante"
	cosmosante "github.com/rollchains/pchain/app/cosmos"
)

// newCosmosAnteHandler creates the default ante handler for Cosmos transactions
func NewCosmosAnteHandler(options HandlerOptions) sdk.AnteHandler {
	return sdk.ChainAnteDecorators(
		NewLoggingDecorator("RejectMessages", evmoscosmosante.NewRejectMessagesDecorator()),
		NewLoggingDecorator("AuthzLimiter", evmoscosmosante.NewAuthzLimiterDecorator(
			sdk.MsgTypeURL(&evmtypes.MsgEthereumTx{}),
			sdk.MsgTypeURL(&sdkvesting.MsgCreateVestingAccount{}),
		)),
		NewLoggingDecorator("SetUpContext", ante.NewSetUpContextDecorator()),
		NewLoggingDecorator("WasmLimitSimulationGas", wasmkeeper.NewLimitSimulationGasDecorator(options.WasmConfig.SimulationGasLimit)),
		NewLoggingDecorator("WasmCountTX", wasmkeeper.NewCountTXDecorator(options.TXCounterStoreService)),
		NewLoggingDecorator("WasmGasRegister", wasmkeeper.NewGasRegisterDecorator(options.WasmKeeper.GetGasRegister())),
		NewLoggingDecorator("CircuitBreaker", circuitante.NewCircuitBreakerDecorator(options.CircuitKeeper)),
		NewLoggingDecorator("ExtensionOptions", ante.NewExtensionOptionsDecorator(options.ExtensionOptionChecker)),
		NewLoggingDecorator("ValidateBasic", ante.NewValidateBasicDecorator()),
		NewLoggingDecorator("TxTimeoutHeight", ante.NewTxTimeoutHeightDecorator()),
		NewLoggingDecorator("ValidateMemo", ante.NewValidateMemoDecorator(options.AccountKeeper)),
		NewLoggingDecorator("MinGasPrice", cosmosante.NewMinGasPriceDecorator(options.FeeMarketKeeper, options.EvmKeeper)),
		NewLoggingDecorator("ConsumeGasForTxSize", ante.NewConsumeGasForTxSizeDecorator(options.AccountKeeper)),
		NewLoggingDecorator("DeductFee", NewDeductFeeDecorator(options.AccountKeeper, options.BankKeeper, options.FeegrantKeeper, options.TxFeeChecker)),
		NewLoggingDecorator("SetPubKey", ante.NewSetPubKeyDecorator(options.AccountKeeper)),
		NewLoggingDecorator("ValidateSigCount", ante.NewValidateSigCountDecorator(options.AccountKeeper)),
		NewLoggingDecorator("SigGasConsume", ante.NewSigGasConsumeDecorator(options.AccountKeeper, options.SigGasConsumer)),
		NewLoggingDecorator("SigVerification", ante.NewSigVerificationDecorator(options.AccountKeeper, options.SignModeHandler)),
		NewLoggingDecorator("IncrementSequence", ante.NewIncrementSequenceDecorator(options.AccountKeeper)),
		NewLoggingDecorator("IBCRedundantRelay", ibcante.NewRedundantRelayDecorator(options.IBCKeeper)),
		NewLoggingDecorator("GasWanted", evmante.NewGasWantedDecorator(options.EvmKeeper, options.FeeMarketKeeper)),
	)
}
