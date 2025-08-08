package keeper

import (
	"context"
	"math/big"
	"strconv"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	pchaintypes "github.com/pushchain/push-chain-node/types"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// updateParams is for updating params collections of the module
func (k Keeper) MintPC(ctx context.Context, evmFrom common.Address, universalAccountId *types.UniversalAccountId, txHash string) error {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	factoryAddress := common.HexToAddress(types.FACTORY_PROXY_ADDRESS_HEX)

	// RPC call verification to get amount to be mint
	amountOfUsdLocked, usdDecimals, err := k.utxverifierKeeper.VerifyAndGetLockedFunds(ctx, universalAccountId.Owner, txHash, universalAccountId.GetCAIP2())
	if err != nil {
		return errors.Wrapf(err, "failed to verify gateway interaction transaction")
	}
	amountToMint := ConvertUsdToPCTokens(&amountOfUsdLocked, usdDecimals)

	// Calling factory contract to compute the UEA address
	receipt, err := k.CallFactoryToComputeUEAAddress(sdkCtx, evmFrom, factoryAddress, universalAccountId)
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

	// Emit derived EVM tx events to simulate an EVM-style mint transaction
	// from the module to the computed UEA address, for indexing and RPC compatibility.
	k.DerivedMintPCEvmTx(sdkCtx, common.HexToAddress(ueaComputedAddress), amountToMint.BigInt())

	return nil
}

// ConvertUsdToPCTokens converts locked USD amount (in wei) to PC tokens (with 18 decimals)
func ConvertUsdToPCTokens(usdAmount *big.Int, usdDecimals uint32) sdkmath.Int {
	// Multiply usdAmount by PC token's conversion rate (10)
	multiplied := new(big.Int).Mul(usdAmount, big.NewInt(10))

	// Scale to 18 decimals (PC token), accounting for usdDecimals
	scaleFactor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(18-usdDecimals)), nil)
	pcTokens := new(big.Int).Mul(multiplied, scaleFactor)

	return sdkmath.NewIntFromBigInt(pcTokens)
}

func (k Keeper) DerivedMintPCEvmTx(ctx context.Context, toAddr common.Address, amount *big.Int) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	uexecutormoduleAcc := k.accountKeeper.GetModuleAccount(ctx, types.ModuleName) // "uexecutor"
	uexecutormoduleAddr := uexecutormoduleAcc.GetAddress()
	var ethSenderAddr common.Address
	copy(ethSenderAddr[:], uexecutormoduleAddr.Bytes())

	msg := ethtypes.NewMessage(
		ethSenderAddr,         // from
		&toAddr,               // to
		0,                     // nonce
		amount,                // amount
		0,                     // gasLimit
		big.NewInt(0),         //  gasPrice
		big.NewInt(0),         // gasFeeCap
		big.NewInt(0),         // gasTipCap
		nil,                   // data
		ethtypes.AccessList{}, // AccessList
		true,                  // isFake
	)
	tx := ethtypes.NewTx(&ethtypes.DynamicFeeTx{
		Nonce:     msg.Nonce(),
		GasFeeCap: msg.GasFeeCap(),
		GasTipCap: msg.GasTipCap(),
		Gas:       msg.Gas(),
		To:        msg.To(),
		Value:     msg.Value(),
		Data:      msg.Data(),
	})
	ethTxHash := tx.Hash()
	attrs := []sdk.Attribute{}
	attrs = append(attrs, []sdk.Attribute{
		sdk.NewAttribute(sdk.AttributeKeyAmount, amount.String()),
		// add event for ethereum transaction hash format;
		sdk.NewAttribute(vmtypes.AttributeKeyEthereumTxHash, ethTxHash.String()),
		// add event for index of valid ethereum tx; NOTE: default txindex for derivedTx
		sdk.NewAttribute(vmtypes.AttributeKeyTxIndex, strconv.FormatUint(vmtypes.DerivedTxIndex, 10)),
		// add event for eth tx gas used, we can't get it from cosmos tx result when it contains multiple eth tx msgs.
		sdk.NewAttribute(vmtypes.AttributeKeyTxGasUsed, "0"),
		// add event for recipient address in evm format;
		sdk.NewAttribute(vmtypes.AttributeKeyRecipient, toAddr.Hex()),
	}...)

	// adding nonce for more info in rpc methods in order to parse derived txs
	attrs = append(attrs, sdk.NewAttribute(vmtypes.AttributeKeyTxNonce, "0"))
	sdkCtx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			vmtypes.EventTypeEthereumTx,
			attrs...,
		),
		sdk.NewEvent(
			vmtypes.EventTypeTxLog,
			sdk.NewAttribute(vmtypes.AttributeKeyTxLog, ""),
		),
		sdk.NewEvent(
			sdk.EventTypeMessage,
			sdk.NewAttribute(sdk.AttributeKeyModule, vmtypes.ModuleName),
			sdk.NewAttribute(sdk.AttributeKeySender, ethSenderAddr.Hex()),
			sdk.NewAttribute(vmtypes.AttributeKeyTxType, strconv.FormatUint(vmtypes.DerivedTxType, 10)),
		),
	})
}
