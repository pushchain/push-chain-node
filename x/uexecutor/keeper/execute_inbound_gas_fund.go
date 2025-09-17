package keeper

import (
	"context"
	// "fmt"
	// "math/big"
	// "strings"

	// sdk "github.com/cosmos/cosmos-sdk/types"
	// "github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	// "github.com/pushchain/push-chain-node/utils"
	// "cosmossdk.io/errors"
)

func (k Keeper) ExecuteInboundGasFund(ctx context.Context, inbound types.Inbound) error {
	// sdkCtx := sdk.UnwrapSDKContext(ctx)

	// tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, inbound.SourceChain, inbound.AssetAddr)
	// if err != nil {
	// 	return err
	// }

	// prc20Address := tokenConfig.NativeRepresentation.ContractAddress

	// Convert inputs
	// prc20AddressHex := common.HexToAddress(prc20Address)
	// amount := new(big.Int)
	// amount, ok := amount.SetString(inbound.Amount, 10) // assuming decimal string
	// if !ok {
	// 	return fmt.Errorf("invalid amount: %s", inbound.Amount)
	// }

	// universalAccountId := types.UniversalAccountId{
	// 	ChainNamespace: strings.Split(inbound.SourceChain, ":")[0],
	// 	ChainId: strings.Split(inbound.SourceChain, ":")[1],
	// 	Owner: inbound.Sender,
	// }

	// factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	// Call to check if uea is deployed
	// ueaAddr, isDeployed, err := k.CallFactoryToGetUEAAddressForOrigin(sdkCtx, ueModuleAccAddress, factoryAddress, &universalAccountId)
	// if err != nil {
	// 	return err
	// }

	// if !isDeployed {
	// 	addrStr, err := k.DeployUEAV2(ctx, ueModuleAccAddress, &universalAccountId)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	ueaAddr = common.HexToAddress(addrStr)
	// }

	// TODO: Call new deposit fn of handler that mints prc20 -> swap -> deposits to recipient
	// receipt, err := k.CallPRC20Deposit(sdkCtx, prc20AddressHex, recipient, amount)
	// if err != nil {
	// 	// TODO: update status to PendingRevert and add revert mechanism here
	// 	return err
	// }

	// _, ueModuleAddressStr := k.GetUeModuleAddress(ctx)

	// universalTxKey := types.GetInboundKey(inbound)
	// err = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
	// 	pcTx := types.PCTx{
	// 		TxHash:      receipt.Hash,       // since tx didn’t go through
	// 		Sender:      ueModuleAddressStr, // or executor’s address if available
	// 		GasUsed:     receipt.GasUsed,
	// 		BlockHeight: uint64(sdkCtx.BlockHeight()),
	// 		Status:      "SUCCESS",
	// 		ErrorMsg:    "",
	// 	}

	// 	utx.PcTx = &pcTx
	// 	utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
	// return nil
	// })
	// if err != nil {
	// 	return err
	// }

	return nil
}
