package keeper

import (
	"context"
	"fmt"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFundsAndPayload(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)
	universalTxKey := types.GetInboundUniversalTxKey(*utx.InboundTx)
	sdkCtx.Logger().Info(
		"ExecuteInboundFundsAndPayload: start",
		"utx_id", utx.Id,
		"universal_tx_key", universalTxKey,
		"source_chain", utx.InboundTx.SourceChain,
		"asset_addr", utx.InboundTx.AssetAddr,
		"amount", utx.InboundTx.Amount,
		"sender", utx.InboundTx.Sender,
		"tx_hash", utx.InboundTx.TxHash,
	)

	shouldRevert := false
	var revertReason string

	// Build universalAccountId
	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}
	sdkCtx.Logger().Info(
		"ExecuteInboundFundsAndPayload: universal account id built",
		"chain_namespace", universalAccountId.ChainNamespace,
		"chain_id", universalAccountId.ChainId,
		"owner", universalAccountId.Owner,
	)

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: factory address resolved", "factory_address", factoryAddress.Hex())

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse

	// --- Step 1: check factory for UEA
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: checking UEA deployment in factory")
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
	if err != nil {
		execErr = fmt.Errorf("factory lookup failed: %w", err)
		shouldRevert = true
		revertReason = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: factory lookup failed", "error", execErr.Error())
	} else if !isDeployed {
		execErr = fmt.Errorf("UEA is not deployed")
		shouldRevert = true
		revertReason = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: UEA not deployed", "uea_addr", ueaAddr.Hex())
	} else {
		sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: UEA resolved", "uea_addr", ueaAddr.Hex())
		// --- Step 2: deposit PRC20 into UEA
		sdkCtx.Logger().Info(
			"ExecuteInboundFundsAndPayload: calling depositPRC20",
			"source_chain", utx.InboundTx.SourceChain,
			"asset_addr", utx.InboundTx.AssetAddr,
			"recipient", ueaAddr.Hex(),
			"amount", utx.InboundTx.Amount,
		)
		receipt, err = k.depositPRC20(
			sdkCtx,
			utx.InboundTx.SourceChain,
			utx.InboundTx.AssetAddr,
			ueaAddr,
			utx.InboundTx.Amount,
		)
		if err != nil {
			execErr = fmt.Errorf("depositPRC20 failed: %w", err)
			shouldRevert = true
			revertReason = execErr.Error()
			sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: depositPRC20 failed", "error", execErr.Error())
		} else {
			sdkCtx.Logger().Info(
				"ExecuteInboundFundsAndPayload: depositPRC20 succeeded",
				"tx_hash", receipt.Hash,
				"gas_used", receipt.GasUsed,
			)
		}
	}

	// --- record deposit attempt
	depositPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if execErr != nil {
		depositPcTx.ErrorMsg = execErr.Error()
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: deposit phase failed", "error", depositPcTx.ErrorMsg)
	} else {
		depositPcTx.TxHash = receipt.Hash
		depositPcTx.GasUsed = receipt.GasUsed
		depositPcTx.Status = "SUCCESS"
		sdkCtx.Logger().Info(
			"ExecuteInboundFundsAndPayload: deposit phase succeeded",
			"tx_hash", depositPcTx.TxHash,
			"gas_used", depositPcTx.GasUsed,
		)
	}
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: updating universal tx with deposit pcTx")
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &depositPcTx)
		if execErr != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		}
		return nil
	})
	if updateErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: failed updating universal tx after deposit", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: universal tx updated after deposit")

	// If deposit failed, stop here (don’t attempt payload execution)
	if execErr != nil {
		if shouldRevert {
			sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: creating revert outbound", "reason", revertReason)
			revertOutbound := &types.OutboundTx{
				DestinationChain: utx.InboundTx.SourceChain,
				Recipient: func() string {
					if utx.InboundTx.RevertInstructions != nil {
						return utx.InboundTx.RevertInstructions.FundRecipient
					}
					return utx.InboundTx.Sender
				}(),
				Amount:            utx.InboundTx.Amount,
				ExternalAssetAddr: utx.InboundTx.AssetAddr,
				Sender:            utx.InboundTx.Sender,
				TxType:            types.TxType_INBOUND_REVERT,
				OutboundStatus:    types.Status_PENDING,
				Id:                types.GetOutboundRevertId(utx.InboundTx.TxHash),
			}

			sdkCtx.Logger().Info(
				"ExecuteInboundFundsAndPayload: attaching revert outbound",
				"outbound_id", revertOutbound.Id,
				"destination_chain", revertOutbound.DestinationChain,
				"recipient", revertOutbound.Recipient,
			)
			_ = k.attachOutboundsToUtx(
				sdkCtx,
				universalTxKey,
				[]*types.OutboundTx{revertOutbound},
				revertReason,
			)
			sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: revert outbound attach attempted")
		}

		sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: done after deposit failure")
		return nil
	}

	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: entering payload phase", "ue_module_addr", ueModuleAddr.Hex())

	// --- Step 3: compute and store payload hash
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: storing verified payload hash")
	payloadHashErr := k.StoreVerifiedPayloadHash(sdkCtx, utx, ueaAddr, ueModuleAddr)
	if payloadHashErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: StoreVerifiedPayloadHash failed", "error", payloadHashErr.Error())
		// Update UniversalTx with payload hash error and stop
		errorPcTx := types.PCTx{
			Sender:      ueModuleAddressStr,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "FAILED",
			ErrorMsg:    fmt.Sprintf("payload hash failed: %v", payloadHashErr),
		}
		_ = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
			utx.PcTx = append(utx.PcTx, &errorPcTx)
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
			return nil
		})
		sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: payload-hash failure pcTx append attempted")
		return nil
	}
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: payload hash stored")

	// --- Step 4: execute payload
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: executing payload")
	receipt, err = k.ExecutePayloadV2(ctx, ueModuleAddr, &universalAccountId, utx.InboundTx.UniversalPayload, utx.InboundTx.VerificationData)

	payloadPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if err != nil {
		payloadPcTx.ErrorMsg = err.Error()
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: ExecutePayloadV2 failed", "error", err.Error())
	} else {
		payloadPcTx.TxHash = receipt.Hash
		payloadPcTx.GasUsed = receipt.GasUsed
		payloadPcTx.Status = "SUCCESS"
		sdkCtx.Logger().Info(
			"ExecuteInboundFundsAndPayload: payload execution succeeded",
			"tx_hash", payloadPcTx.TxHash,
			"gas_used", payloadPcTx.GasUsed,
		)

		if receipt != nil {
			sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: attaching outbound(s) from payload receipt")
			_ = k.AttachOutboundsToExistingUniversalTx(sdkCtx, receipt, utx)
			sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: outbound attach attempted")
		}
	}

	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: updating universal tx with payload pcTx")
	updateErr = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &payloadPcTx)
		if err != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		} else {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		}
		return nil
	})
	if updateErr != nil {
		sdkCtx.Logger().Error("ExecuteInboundFundsAndPayload: failed updating universal tx after payload", "error", updateErr.Error())
		return updateErr
	}
	sdkCtx.Logger().Info("ExecuteInboundFundsAndPayload: done", "payload_error", err != nil)

	return nil
}
