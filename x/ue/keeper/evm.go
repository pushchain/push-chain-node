package keeper

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/evmos/os/x/evm/types"
	"github.com/rollchains/pchain/x/ue/types"
)

// CallFactoryToComputeUEAAddress calls FactoryV1.computeUEA(...)
func (k Keeper) CallFactoryToComputeUEAAddress(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	universalAccountId *types.UniversalAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	abiUniversalAccountId, err := types.NewAbiUniversalAccountId(universalAccountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal account")
	}

	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		factoryAddr,
		false, // commit
		"computeUEA",
		abiUniversalAccountId,
	)
}

// CallFactoryToDeployUEA deploys a new UEA using factory contract
// Returns deployment response or error if deployment fails
func (k Keeper) CallFactoryToDeployUEA(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	universalAccountId *types.UniversalAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	abiUniversalAccountId, err := types.NewAbiUniversalAccountId(universalAccountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal account")
	}

	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,        // who is sending the transaction
		factoryAddr, // destination: FactoryV1 contract
		true,        // commit = true (real tx, not simulation)
		"deployUEA",
		abiUniversalAccountId,
	)
}

// CallUEAExecutePayload executes a universal payload through UEA
func (k Keeper) CallUEAExecutePayload(
	ctx sdk.Context,
	from, ueaAddr common.Address,
	universal_payload *types.UniversalPayload,
	signature []byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseUeaABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UEA ABI")
	}

	abiUniversalPayload, err := types.NewAbiUniversalPayload(universal_payload)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal payload")
	}

	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		ueaAddr,
		true, // commit
		"executePayload",
		abiUniversalPayload,
		signature,
	)
}
