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
	caip string,
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
		caip,
	)
}

// CallFactoryToDeployNMSC deploys a new smart account using factory contract
// Returns deployment response or error if deployment fails
func (k Keeper) CallFactoryToDeployNMSC(
	ctx sdk.Context,
	from, factoryAddr, verifierPrecompile common.Address,
	userKey []byte,
	caip string,
	ownerType uint8,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}
	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,        // who is sending the transaction
		factoryAddr, // destination: your FactoryV1 contract
		true,        // commit = true (you want real tx, not simulation)
		"deploySmartAccount",
		userKey,
		caip,
		ownerType,
		verifierPrecompile,
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
