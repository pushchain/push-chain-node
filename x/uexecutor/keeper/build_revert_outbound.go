package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// buildRevertOutbound creates an INBOUND_REVERT outbound with gas fields populated
// from the UniversalCore contract via getOutboundTxGasAndFees.
func (k Keeper) buildRevertOutbound(sdkCtx sdk.Context, inbound *types.Inbound) *types.OutboundTx {
	recipient := inbound.Sender
	if inbound.RevertInstructions != nil && inbound.RevertInstructions.FundRecipient != "" {
		recipient = inbound.RevertInstructions.FundRecipient
	}

	outbound := &types.OutboundTx{
		DestinationChain:  inbound.SourceChain,
		Recipient:         recipient,
		Amount:            inbound.Amount,
		ExternalAssetAddr: inbound.AssetAddr,
		Sender:            inbound.Sender,
		TxType:            types.TxType_INBOUND_REVERT,
		OutboundStatus:    types.Status_PENDING,
		Id:                types.GetOutboundRevertId(inbound.SourceChain, inbound.TxHash, inbound.LogIndex),
	}

	// PC20 return revert: ExternalAssetAddr is the wrapper, which has no PRC20 token
	// config — the external Vault.revertUniversalTx re-mints it (revertMint) keyed off
	// that address. Skip the PRC20 gas lookup (it would only warn and return here for a
	// wrapper) and record the wrapper in Pc20ContractAddress. Do NOT flag IsPc20: a
	// failed INBOUND_REVERT must never route into handleFailedOutbound's revertExport,
	// which is only for a failed export.
	if inbound.IsPc20 {
		outbound.Pc20ContractAddress = inbound.AssetAddr
		return outbound
	}

	// Look up the PRC20 address for this external token
	tokenCfg, err := k.uregistryKeeper.GetTokenConfig(sdkCtx, inbound.SourceChain, inbound.AssetAddr)
	if err != nil || tokenCfg.NativeRepresentation == nil || tokenCfg.NativeRepresentation.ContractAddress == "" {
		k.Logger().Warn("failed to get PRC20 for revert outbound gas lookup, proceeding without gas fields",
			"chain", inbound.SourceChain,
			"asset", inbound.AssetAddr,
			"error", err,
		)
		return outbound
	}

	// Fetch gas fields from UniversalCore.getOutboundTxGasAndFees(prc20, 0)
	// 0 means use the contract's baseLimit for this chain
	gasToken, gasFee, gasPrice, gasLimit, err := k.GetGasFeeInfoForRevertOutbound(sdkCtx, tokenCfg.NativeRepresentation.ContractAddress)
	if err != nil {
		k.Logger().Warn("failed to fetch gas fee info for revert outbound, proceeding without gas fields",
			"chain", inbound.SourceChain,
			"prc20", tokenCfg.NativeRepresentation.ContractAddress,
			"error", err,
		)
		return outbound
	}

	outbound.GasToken = gasToken
	outbound.GasFee = gasFee
	outbound.GasPrice = gasPrice
	outbound.GasLimit = gasLimit

	return outbound
}
