package keeper

import (
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/x/ue/types"
)

// CallFactoryToComputeUEAAddress calls FactoryV1.computeUEA(...)
func (k Keeper) CallFactoryToComputeUEAAddress(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	universalAccount *types.UniversalAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	abiUniversalAccount, err := types.NewAbiUniversalAccountId(universalAccount)
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
		abiUniversalAccount,
	)
}

// CallFactoryToDeployUEA deploys a new UEA using factory contract
// Returns deployment response or error if deployment fails
func (k Keeper) CallFactoryToDeployUEA(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	universalAccount *types.UniversalAccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	abiUniversalAccount, err := types.NewAbiUniversalAccountId(universalAccount)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal account")
	}

	fmt.Println("FROM: ", from)

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		from,        // who is sending the transaction
		factoryAddr, // destination: FactoryV1 contract
		big.NewInt(0),
		true,  // commit = true (real tx, not simulation)
		false, // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		"deployUEA",
		abiUniversalAccount,
	)
}

// CallUEAExecutePayload executes a universal payload through UEA
func (k Keeper) CallUEAExecutePayload(
	ctx sdk.Context,
	from, ueaAddr common.Address,
	universal_payload *types.UniversalPayload,
	verificationData []byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseUeaABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UEA ABI")
	}

	abiUniversalPayload, err := types.NewAbiUniversalPayload(universal_payload)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal payload")
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		from,
		ueaAddr,
		big.NewInt(0),
		true,  // commit = true (real tx, not simulation)
		false, // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		"executePayload",
		abiUniversalPayload,
		verificationData,
	)
}
