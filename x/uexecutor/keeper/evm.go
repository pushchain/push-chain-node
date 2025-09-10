package keeper

import (
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// CallFactoryToGetUEAAddressForOrigin calls FactoryV1.getUEAForOrigin(...)
func (k Keeper) CallFactoryToGetUEAAddressForOrigin(
	ctx sdk.Context,
	from, factoryAddr common.Address,
	universalAccount *types.UniversalAccountId,
) (common.Address, bool, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return common.Address{}, false, errors.Wrap(err, "failed to parse factory ABI")
	}

	abiUniversalAccount, err := types.NewAbiUniversalAccountId(universalAccount)
	if err != nil {
		return common.Address{}, false, errors.Wrapf(err, "failed to create universal account")
	}

	receipt, err := k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		factoryAddr,
		false, // commit
		"getUEAForOrigin",
		abiUniversalAccount,
	)
	if err != nil {
		return common.Address{}, false, err
	}

	results, err := abi.Methods["getUEAForOrigin"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return common.Address{}, false, errors.Wrap(err, "failed to decode result")
	}

	ueaAddress := results[0].(common.Address)
	isDeployed := results[1].(bool)

	return ueaAddress, isDeployed, nil
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
		nil,
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

	gasLimit := new(big.Int)
	gasLimit, ok := gasLimit.SetString(universal_payload.GasLimit, 10)
	if !ok {
		return nil, fmt.Errorf("invalid gas limit: %s", universal_payload.GasLimit)
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		from,
		ueaAddr,
		big.NewInt(0),
		gasLimit,
		true,  // commit = true (real tx, not simulation)
		false, // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		"executePayload",
		abiUniversalPayload,
		verificationData,
	)
}

// Calls Handler Contract to deposit prc20 tokens
func (k Keeper) CallPRC20Deposit(
	ctx sdk.Context,
	prc20Address, to common.Address,
	amount *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	// fetch system config
	sysCfg, err := k.uregistryKeeper.GetSystemConfig(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get system config")
	}

	if sysCfg.HandlerContractAddress == "" {
		return nil, fmt.Errorf("handler contract address not set in system config")
	}

	handlerAddr := common.HexToAddress(sysCfg.HandlerContractAddress)

	abi, err := types.ParseHandlerABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Handler Contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress, // who is sending the transaction
		handlerAddr,        // destination: Handler contract
		big.NewInt(0),
		nil,
		true,  // commit = true (real tx, not simulation)
		false, // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		"depositPRC20Token",
		prc20Address,
		amount,
		to,
	)
}
