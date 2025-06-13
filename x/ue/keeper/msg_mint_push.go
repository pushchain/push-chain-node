package keeper

import (
	"context"

	"math/big"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/x/ue/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) mintPush(ctx context.Context, evmFrom common.Address, universalAccount *types.UniversalAccount, txHash string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	factoryAddress := common.HexToAddress(types.FACTORY_ADDRESS_HEX)

	// RPC call verification to get amount to be mint
	amountOfUsdLocked, usdDecimals, err := k.utvKeeper.VerifyAndGetLockedFunds(ctx, universalAccount.Owner, txHash, universalAccount.Chain)
	if err != nil {
		return errors.Wrapf(err, "failed to verify locker interaction transaction")
	}
	amountToMint := ConvertUsdToPushTokens(&amountOfUsdLocked, usdDecimals)

	// Calling factory contract to compute the UEA address
	receipt, err := k.CallFactoryToComputeUEAAddress(sdkCtx, evmFrom, factoryAddress, universalAccount)
	if err != nil {
		return err
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	ueaComputedAddress := "0x" + addressBytes

	// Convert the computed address to a Cosmos address
	cosmosAddr, err := utils.ConvertAnyAddressToBytes(ueaComputedAddress)

	if err != nil {
		return errors.Wrapf(err, "failed to convert EVM address to Cosmos address")
	}

	err = k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint)))
	if err != nil {
		return errors.Wrapf(err, "failed to mint coins")
	}

	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, cosmosAddr, sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint)))
	if err != nil {
		return errors.Wrapf(err, "failed to send coins from module to account")
	}

	return nil
}

// ConvertUsdToPushTokens converts locked USD amount (in wei) to PUSH tokens (with 18 decimals)
func ConvertUsdToPushTokens(usdAmount *big.Int, usdDecimals uint32) sdkmath.Int {
	// Multiply usdAmount by PC token's conversion rate (10)
	multiplied := new(big.Int).Mul(usdAmount, big.NewInt(10))

	// Scale to 18 decimals (PC token), accounting for usdDecimals
	scaleFactor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-usdDecimals)), nil)
	pushTokens := new(big.Int).Mul(multiplied, scaleFactor)

	return sdkmath.NewIntFromBigInt(pushTokens)
}
