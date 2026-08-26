package keeper

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// buildRevertOutbound creates an INBOUND_REVERT outbound that returns a failed
// inbound's funds on the source chain.
//
// The gas fields (gas token / fee / price / limit) are resolved from the
// UniversalCore contract and are mandatory: the universal validators refuse to
// sign an outbound whose gas price is zero or missing, so a revert built without
// them can never be broadcast, and re-resolving the metadata later does not
// rewrite the fields already stored on the outbound.
//
// Failure to resolve them is therefore never silent. The outbound is returned
// marked Status_ABORTED with an AbortReason instead of Status_PENDING, together
// with a non-nil error describing what failed:
//
//   - it is still worth recording. The attempt stays in the audit trail and it
//     makes the universal tx eligible for RESCUE_FUNDS, which is the recovery
//     route for funds that never made it back to the user.
//   - it must never be queued for signing. attachOutboundsToUtx enforces that by
//     indexing only PENDING outbounds into PendingOutbounds.
//
// A nil outbound together with a non-nil error means nothing could be built at all.
func (k Keeper) buildRevertOutbound(sdkCtx sdk.Context, inbound *types.Inbound) (*types.OutboundTx, error) {
	if inbound == nil {
		return nil, fmt.Errorf("cannot build revert outbound: inbound is nil")
	}

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

	// Look up the PRC20 address for this external token
	tokenCfg, err := k.uregistryKeeper.GetTokenConfig(sdkCtx, inbound.SourceChain, inbound.AssetAddr)
	if err != nil || tokenCfg.NativeRepresentation == nil || tokenCfg.NativeRepresentation.ContractAddress == "" {
		lookupErr := err
		if lookupErr == nil {
			lookupErr = fmt.Errorf("token config has no native representation")
		}
		abortErr := fmt.Errorf("failed to resolve PRC20 for revert outbound of %s on %s: %w",
			inbound.AssetAddr, inbound.SourceChain, lookupErr)

		k.Logger().Error("revert outbound aborted: PRC20 lookup failed",
			"chain", inbound.SourceChain,
			"asset", inbound.AssetAddr,
			"outbound_id", outbound.Id,
			"error", abortErr.Error(),
		)

		abortRevertOutbound(outbound, abortErr)
		return outbound, abortErr
	}

	// Fetch gas fields from UniversalCore.getOutboundTxGasAndFees(prc20, 0)
	// 0 means use the contract's baseLimit for this chain
	gasToken, gasFee, gasPrice, gasLimit, err := k.GetGasFeeInfoForRevertOutbound(sdkCtx, tokenCfg.NativeRepresentation.ContractAddress)
	if err != nil {
		abortErr := fmt.Errorf("failed to fetch gas fee info for revert outbound of PRC20 %s on %s: %w",
			tokenCfg.NativeRepresentation.ContractAddress, inbound.SourceChain, err)

		k.Logger().Error("revert outbound aborted: gas fee lookup failed",
			"chain", inbound.SourceChain,
			"prc20", tokenCfg.NativeRepresentation.ContractAddress,
			"outbound_id", outbound.Id,
			"error", abortErr.Error(),
		)

		abortRevertOutbound(outbound, abortErr)
		return outbound, abortErr
	}

	outbound.GasToken = gasToken
	outbound.GasFee = gasFee
	outbound.GasPrice = gasPrice
	outbound.GasLimit = gasLimit

	return outbound, nil
}

// abortRevertOutbound marks a half-built revert outbound as ABORTED with a reason.
// It mirrors the shape AbortOutbound writes for outbounds that are already attached
// to a universal tx; the matching outbound_aborted event is emitted by
// attachOutboundsToUtx, which is where the universal tx id is known.
func abortRevertOutbound(outbound *types.OutboundTx, reason error) {
	outbound.OutboundStatus = types.Status_ABORTED
	outbound.AbortReason = reason.Error()
	outbound.GasToken = ""
	outbound.GasFee = ""
	outbound.GasPrice = ""
	outbound.GasLimit = ""
}
