package keeper

import (
	"fmt"
	"math/big"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
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
		false, // not a module sender
		nil,
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
		false, // not a module sender
		nil,
		"executePayload",
		abiUniversalPayload,
		verificationData,
	)
}

// CallUEAMigrateUEA migrates UEA through existing UEA
func (k Keeper) CallUEAMigrateUEA(
	ctx sdk.Context,
	from, ueaAddr common.Address,
	migration_payload *types.MigrationPayload,
	signature []byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	abi, err := types.ParseUeaABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UEA ABI")
	}

	abiMigrationPayload, err := types.NewAbiMigrationPayload(migration_payload)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create universal payload")
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		from,
		ueaAddr,
		big.NewInt(0),
		big.NewInt(21000000),
		true,  // commit = true (real tx, not simulation)
		true,  // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		false, // not a module sender
		nil,
		"migrateUEA",
		abiMigrationPayload,
		signature,
	)
}

// CallUEADomainSeparator fetches the domainSeparator from the UEA contract
func (k Keeper) CallUEADomainSeparator(
	ctx sdk.Context,
	from, ueaAddr common.Address,
) ([32]byte, error) {
	abi, err := types.ParseUeaABI()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "failed to parse UEA ABI")
	}
	// Call the view function domainSeparator()
	res, err := k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		ueaAddr,
		false, // commit = false (static call)
		"domainSeparator",
	)
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "failed to call domainSeparator")
	}
	// Convert returned bytes to [32]byte
	if len(res.Ret) < 32 {
		return [32]byte{}, fmt.Errorf("invalid domainSeparator length: got %d, want 32", len(res.Ret))
	}
	var separator [32]byte
	copy(separator[:], res.Ret[:32])

	return separator, nil
}

// Calls Handler Contract to deposit prc20 tokens
func (k Keeper) CallPRC20Deposit(
	ctx sdk.Context,
	prc20Address, to common.Address,
	amount *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Handler Contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	// Before sending an EVM tx from module
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}

	// increment first (safe for internal modules)
	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress, // sender: module account
		handlerAddr,        // destination
		big.NewInt(0),
		nil,
		true,   // commit = true (real tx, not simulation)
		false,  // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		true,   // module sender = true
		&nonce, // manual nonce of module
		"depositPRC20Token",
		prc20Address,
		amount,
		to,
	)
}

// Calls UniversalCore Contract to set gas price
func (k Keeper) CallUniversalCoreSetGasPrice(
	ctx sdk.Context,
	chainID string,
	price *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Handler Contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	// Before sending an EVM tx from module
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}

	// increment first (safe for internal modules)
	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress, // who is sending the transaction
		handlerAddr,        // destination: Handler contract
		big.NewInt(0),
		nil,
		true,   // commit = true (real tx, not simulation)
		false,  // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		true,   // module sender = true
		&nonce, // manual nonce of module
		"setGasPrice",
		chainID,
		price,
	)
}

// Calls Handler Contract to deposit prc20 tokens
func (k Keeper) CallPRC20DepositAutoSwap(
	ctx sdk.Context,
	prc20Address, to common.Address,
	amount *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Handler Contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	// Before sending an EVM tx from module
	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}

	// increment first (safe for internal modules)
	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress, // who is sending the transaction
		handlerAddr,        // destination: Handler contract
		big.NewInt(0),
		nil,
		true,   // commit = true (real tx, not simulation)
		false,  // gasless = false (@dev: we need gas to be emitted in the tx receipt)
		true,   // module sender = true
		&nonce, // manual nonce of module
		"depositPRC20WithAutoSwap",
		prc20Address,
		amount,
		to,
		big.NewInt(0),
		big.NewInt(0),
		big.NewInt(0),
	)
}
