package keeper

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	evmtypes "github.com/evmos/os/x/evm/types"
	"github.com/rollchains/pchain/x/ue/types"
)

// CallFactoryToComputeAddress calls FactoryV1.computeSmartAccountAddress(...)
func (k Keeper) CallFactoryToComputeAddress(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	accountId *types.AccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	accountID, err := types.NewAbiAccountId(accountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create accountId")
	}

	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		factoryAddr,
		false, // commit
		"computeSmartAccountAddress",
		accountID,
	)
}

// CallFactoryToDeployNMSC deploys a new smart account using factory contract
// Returns deployment response or error if deployment fails
func (k Keeper) CallFactoryToDeployNMSC(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	accountId *types.AccountId,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse factory ABI")
	}

	accountID, err := types.NewAbiAccountId(accountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create accountId")
	}

	return k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,        // who is sending the transaction
		factoryAddr, // destination: FactoryV1 contract
		true,        // commit = true (real tx, not simulation)
		"deploySmartAccount",
		accountID,
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
