package keeper

import (
	"context"
	"strings"

	"cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInboundFundsAndPayload(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	universalAccountId := types.UniversalAccountId{
		ChainNamespace: strings.Split(utx.InboundTx.SourceChain, ":")[0],
		ChainId:        strings.Split(utx.InboundTx.SourceChain, ":")[1],
		Owner:          utx.InboundTx.Sender,
	}

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	// Call to check if uea is deployed
	ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
	if err != nil {
		return err
	}

	if !isDeployed {
		return errors.Wrapf(err, "UEA is not deployed")
	}

	receipt, err := k.depositPRC20(
		sdkCtx,
		utx.InboundTx.SourceChain,
		utx.InboundTx.AssetAddr,
		ueaAddr, // recipient is uea itself
		utx.InboundTx.Amount,
	)
	if err != nil {
		// TODO: update status to PendingRevert and add revert mechanism here
		return err
	}

	ueModuleAddr, ueModuleAddressStr := k.GetUeModuleAddress(ctx)

	universalTxKey := types.GetInboundKey(*utx.InboundTx)
	err = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		pcTx := types.PCTx{
			TxHash:      receipt.Hash,       // TODO: handle revert params
			Sender:      ueModuleAddressStr, // or executor’s address if available
			GasUsed:     receipt.GasUsed,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "SUCCESS", // TODO: handler revert params
			ErrorMsg:    "",        // TODO: handler revert params
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		// utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		return nil
	})
	if err != nil {
		return err // TODO: revert mechanism
	}

	receipt, err = k.ExecutePayloadV2(ctx, ueModuleAddr, &universalAccountId, utx.InboundTx.UniversalPayload, utx.InboundTx.VerificationData)
	if err != nil {
		return err // TODO: revert mechanism
	}
	err = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		pcTx := types.PCTx{
			TxHash:      receipt.Hash, // TODO: handler revert params
			Sender:      ueModuleAddressStr,
			GasUsed:     receipt.GasUsed,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "SUCCESS", // TODO: handler revert params
			ErrorMsg:    "",        // TODO: handler revert params
		}

		utx.PcTx = append(utx.PcTx, &pcTx)
		utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		return nil
	})
	if err != nil {
		return err
	}

	return nil
}
