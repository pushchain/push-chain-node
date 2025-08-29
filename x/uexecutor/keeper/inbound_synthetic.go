package keeper

import (
	"context"
<<<<<<< HEAD:x/uexecutor/keeper/inbound_synthetic.go

	"github.com/rollchains/pchain/x/ue/types"
)

// AddPendingInboundSynthetic adds an inbound synthetic to the pending set if not already present
func (k Keeper) AddPendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound)
	has, err := k.PendingInboundSynthetics.Has(ctx, key)
	if err != nil {
		return err
	}
	if has {
		// Already present, do nothing
		return nil
	}
	return k.PendingInboundSynthetics.Set(ctx, key)
}

// IsPendingInboundSynthetic checks if an inbound synthetic is pending
func (k Keeper) IsPendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) (bool, error) {
	key := types.GetInboundSyntheticKey(inbound)
	return k.PendingInboundSynthetics.Has(ctx, key)
}

// RemovePendingInboundSynthetic removes an inbound synthetic from the pending set
func (k Keeper) RemovePendingInboundSynthetic(ctx context.Context, inbound types.InboundSynthetic) error {
	key := types.GetInboundSyntheticKey(inbound)
	return k.PendingInboundSynthetics.Remove(ctx, key)
=======
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/x/ue/types"
)

func (k Keeper) ExecuteInboundSynthetic(ctx context.Context, utx types.UniversalTx) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, utx.InboundTx.SourceChain, utx.InboundTx.AssetAddr)
	if err != nil {
		return err
	}

	prc20Address := tokenConfig.NativeRepresentation.ContractAddress

	// Convert inputs
	prc20AddressHex := common.HexToAddress(prc20Address)
	recipient := common.HexToAddress(utx.InboundTx.Recipient) // adjust field name
	amount := new(big.Int)
	amount, ok := amount.SetString(utx.InboundTx.Amount, 10) // assuming decimal string
	if !ok {
		return fmt.Errorf("invalid amount: %s", utx.InboundTx.Amount)
	}

	receipt, err := k.CallPRC20Deposit(sdkCtx, prc20AddressHex, recipient, amount)
	if err != nil {
		// TODO: update status to PendingRevert and add revert mechanism here
		return err
	}

	_, ueModuleAddressStr := k.GetUeModuleAddress(ctx)

	universalTxKey := types.GetInboundKey(*utx.InboundTx)
	err = k.UpdateUniversalTx(ctx, universalTxKey, func(utx *types.UniversalTx) error {
		pcTx := types.PCTx{
			TxHash:      receipt.Hash,       // since tx didn’t go through
			Sender:      ueModuleAddressStr, // or executor’s address if available
			GasUsed:     receipt.GasUsed,
			BlockHeight: uint64(sdkCtx.BlockHeight()),
			Status:      "SUCCESS",
			ErrorMsg:    "",
		}

		utx.PcTx = &pcTx
		utx.UniversalStatus = types.UniversalTxStatus_PC_EXECUTED_SUCCESS
		return nil
	})
	if err != nil {
		return err
	}

	return nil
>>>>>>> 51ad333 (feat: added keeper method for ExecuteInboundSynthetic):x/ue/keeper/inbound_synthetic.go
}
