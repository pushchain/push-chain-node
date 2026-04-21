package keeper

import (
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// GasFeeInfo holds gas-related fields fetched from the UniversalCore contract.
type GasFeeInfo struct {
	GasToken common.Address
	GasFee   *big.Int
	GasPrice *big.Int
	GasLimit *big.Int
}

// GetOutboundTxGasAndFees calls UniversalCore.getOutboundTxGasAndFees(prc20, gasLimitWithBaseLimit)
// to get gasToken, gasFee, protocolFee, gasPrice, and chainNamespace.
// Pass gasLimitWithBaseLimit=0 to use the contract's baseLimit.
func (k Keeper) GetOutboundTxGasAndFees(ctx sdk.Context, prc20 common.Address, gasLimitWithBaseLimit *big.Int) (*GasFeeInfo, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	ucABI, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UniversalCore ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	receipt, err := k.evmKeeper.CallEVM(ctx, ucABI, ueModuleAccAddress, handlerAddr, false,
		"getOutboundTxGasAndFees", prc20, gasLimitWithBaseLimit)
	if err != nil {
		return nil, errors.Wrap(err, "failed to call getOutboundTxGasAndFees")
	}

	results, err := ucABI.Methods["getOutboundTxGasAndFees"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unpack getOutboundTxGasAndFees result")
	}

	gasToken := results[0].(common.Address)
	gasFee := results[1].(*big.Int)
	// protocolFee := results[2].(*big.Int) — not needed for outbound fields
	gasPrice := results[3].(*big.Int)

	// Derive gasLimit from gasFee / gasPrice
	var gasLimit *big.Int
	if gasPrice.Sign() > 0 {
		gasLimit = new(big.Int).Div(gasFee, gasPrice)
	} else {
		gasLimit = big.NewInt(0)
	}

	return &GasFeeInfo{
		GasToken: gasToken,
		GasFee:   gasFee,
		GasPrice: gasPrice,
		GasLimit: gasLimit,
	}, nil
}

// GetGasFeeInfoForRevertOutbound fetches gas info for an INBOUND_REVERT outbound using the
// inbound's PRC20 token address. Returns string values ready for OutboundTx fields.
func (k Keeper) GetGasFeeInfoForRevertOutbound(ctx sdk.Context, prc20Addr string) (gasToken, gasFee, gasPrice, gasLimit string, err error) {
	prc20 := common.HexToAddress(prc20Addr)
	info, err := k.GetOutboundTxGasAndFees(ctx, prc20, big.NewInt(0)) // 0 = use baseLimit
	if err != nil {
		return "", "", "", "", fmt.Errorf("failed to get gas fee info: %w", err)
	}

	return info.GasToken.Hex(), info.GasFee.String(), info.GasPrice.String(), info.GasLimit.String(), nil
}
