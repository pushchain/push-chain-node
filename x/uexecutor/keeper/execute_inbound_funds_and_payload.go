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
	universalTxKey := types.GetInboundKey(*utx.InboundTx)

	// Build universalAccountId
	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)
	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	var execErr error
	var receipt *evmtypes.MsgEthereumTxResponse

	// --- Step 1: check factory for UEA
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
	if err != nil {
		execErr = fmt.Errorf("factory lookup failed: %w", err)
	} else if !isDeployed {
		execErr = fmt.Errorf("UEA is not deployed")
	} else {
		// --- Step 2: deposit PRC20 into UEA
		receipt, err = k.depositPRC20(
			sdkCtx,
			utx.InboundTx.SourceChain,
			utx.InboundTx.AssetAddr,
			ueaAddr,
			utx.InboundTx.Amount,
		)
		if err != nil {
			execErr = fmt.Errorf("depositPRC20 failed: %w", err)
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
	} else {
		depositPcTx.TxHash = receipt.Hash
		depositPcTx.GasUsed = receipt.GasUsed
		depositPcTx.Status = "SUCCESS"
	}
	updateErr := k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		utx.PcTx = append(utx.PcTx, &depositPcTx)
		if execErr != nil {
			utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_FAILED
		}
		return nil
	})
	if updateErr != nil {
		return updateErr
	}

	// If deposit failed, stop here (don’t attempt payload execution)
	if execErr != nil {
		return nil
	}

	// --- Step 3: execute payload
	ueModuleAddr, _ := k.GetUeModuleAddress(ctx)
	receipt, err = k.ExecutePayloadV2(ctx, ueModuleAddr, &universalAccountId, utx.InboundTx.UniversalPayload, utx.InboundTx.VerificationData)

	payloadPcTx := types.PCTx{
		Sender:      ueModuleAddressStr,
		BlockHeight: uint64(sdkCtx.BlockHeight()),
		Status:      "FAILED",
	}
	if err != nil {
		payloadPcTx.ErrorMsg = err.Error()
	} else {
		payloadPcTx.TxHash = receipt.Hash
		payloadPcTx.GasUsed = receipt.GasUsed
		payloadPcTx.Status = "SUCCESS"
	}

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
		return updateErr
	}

	// never return execErr or err
	return nil
}
