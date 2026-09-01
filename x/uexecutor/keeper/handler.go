package keeper

import (
	"fmt"
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	vmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) depositPRC20(
	ctx sdk.Context,
	sourceChain string,
	assetAddr string,
	recipient common.Address,
	amountStr string,
) (*vmtypes.MsgEthereumTxResponse, error) {
	// get token config
	tokenConfig, err := k.uregistryKeeper.GetTokenConfig(ctx, sourceChain, assetAddr)
	if err != nil {
		return nil, err
	}

	if tokenConfig.NativeRepresentation == nil {
		return nil, fmt.Errorf("token config for %s:%s has no native representation", sourceChain, assetAddr)
	}
	prc20Address := tokenConfig.NativeRepresentation.ContractAddress
	prc20AddressHex := common.HexToAddress(prc20Address)

	// convert amount
	amount := new(big.Int)
	amount, ok := amount.SetString(amountStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amountStr)
	}

	k.Logger().Debug("EVM call: depositPRC20Token",
		"prc20", prc20AddressHex.Hex(),
		"recipient", recipient.Hex(),
		"amount", amountStr,
	)

	// call PRC20 deposit
	return k.CallPRC20Deposit(ctx, prc20AddressHex, recipient, amount)
}

// unlockPC20 is the PC20-return counterpart of depositPRC20: instead of minting a
// PRC20, it releases the Push-native token that was locked in VaultPC20 on export.
// The inbound carries the external wrapper address (wrapperAddr); the Push-native
// source is resolved via UniversalCore's reverse registry (getPC20Source), then
// VaultPC20.unlock sends it to the recipient. universalTxId is the UTX id, used as
// VaultPC20's single-shot replay guard (subTxId). A missing wrapper mapping is a
// hard error so the caller can revert (re-mint the burned wrapper).
func (k Keeper) unlockPC20(
	ctx sdk.Context,
	sourceChain string,
	wrapperAddr string,
	recipient common.Address,
	amountStr string,
	universalTxId string,
) (*vmtypes.MsgEthereumTxResponse, error) {
	// The wrapper arrives in the source chain's native format (EVM hex / SVM base58);
	// encode it to the bytes32 key UniversalCore uses for the reverse lookup.
	wrapper, err := utils.AddressToBytes32(sourceChain, wrapperAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid PC20 wrapper %s on %s: %w", wrapperAddr, sourceChain, err)
	}

	source, known, err := k.CallUniversalCoreGetPC20Source(ctx, wrapper, sourceChain)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve PC20 source for %s on %s: %w", wrapperAddr, sourceChain, err)
	}
	if !known {
		return nil, fmt.Errorf("no PC20 wrapper mapping for %s on %s — cannot resolve source to unlock", wrapperAddr, sourceChain)
	}

	amount := new(big.Int)
	amount, ok := amount.SetString(amountStr, 10)
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amountStr)
	}

	k.Logger().Debug("EVM call: VaultPC20.unlock",
		"wrapper", wrapperAddr,
		"source", source.Hex(),
		"recipient", recipient.Hex(),
		"amount", amountStr,
	)

	return k.CallVaultPC20Unlock(ctx, common.HexToHash(universalTxId), source, recipient, amount)
}

// creditInboundFunds routes the funds step of an inbound: a PC20 return unlocks
// the Push-native token locked in VaultPC20, everything else mints/deposits the
// PRC20. universalTxId is the UTX id, used as VaultPC20's replay guard on the PC20
// path. Both paths credit the same recipient the PRC20 deposit would have.
func (k Keeper) creditInboundFunds(
	ctx sdk.Context,
	in *types.Inbound,
	universalTxId string,
	recipient common.Address,
) (*vmtypes.MsgEthereumTxResponse, error) {
	if in.IsPc20 {
		return k.unlockPC20(ctx, in.SourceChain, in.AssetAddr, recipient, in.Amount, universalTxId)
	}
	return k.depositPRC20(ctx, in.SourceChain, in.AssetAddr, recipient, in.Amount)
}
