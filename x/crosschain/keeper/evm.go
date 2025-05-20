package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/evmos/os/x/evm/types"
	"github.com/pkg/errors"
	"github.com/rollchains/pchain/x/crosschain/types"
)

// CallFactoryToComputeAddress calls FactoryV1.computeSmartAccountAddress(...)
func (k Keeper) CallFactoryToComputeAddress(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	accountId types.AbiAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}
	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		factoryAddr,
		false, // commit
		"computeSmartAccountAddress",
		accountId,
	)
}

// CallFactoryToDeployNMSC deploys a new smart account using factory contract
// Returns deployment response or error if deployment fails
func (k Keeper) CallFactoryToDeployNMSC(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	accountId types.AbiAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}
	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,        // who is sending the transaction
		factoryAddr, // destination: FactoryV1 contract
		true,        // commit = true (real tx, not simulation)
		"deploySmartAccount",
		accountId,
	)
}

// CallNMSCExecutePayload executes a cross-chain payload through smart account
func (k Keeper) CallNMSCExecutePayload(
	ctx sdk.Context,
	from, nmscAddr common.Address,
	payload types.AbiCrossChainPayload,
	signature []byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseSmartAccountABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse smart account ABI")
	}
	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		nmscAddr,
		true, // commit
		"executePayload",
		payload,
		signature,
	)
}
