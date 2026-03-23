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

// CallFactoryGetOriginForUEA checks if a given address is a UEA
// Returns the UniversalAccountId and a boolean indicating if the address is a UEA
func (k Keeper) CallFactoryGetOriginForUEA(
	ctx sdk.Context,
	from, factoryAddr, ueaAddr common.Address,
) (*types.UniversalAccountId, bool, error) {
	abi, err := types.ParseFactoryABI()
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to parse factory ABI")
	}

	receipt, err := k.evmKeeper.CallEVM(
		ctx,
		abi,
		from,
		factoryAddr,
		false, // commit
		"getOriginForUEA",
		ueaAddr,
	)
	if err != nil {
		return nil, false, err
	}

	results, err := abi.Methods["getOriginForUEA"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return nil, false, errors.Wrap(err, "failed to decode result")
	}

	// Extract the UniversalAccountId from the result
	// The first return value is a struct (UniversalAccountId)
	// The second return value is bool (isUEA)
	isUEA := results[1].(bool)

	if !isUEA {
		return nil, false, nil
	}

	// Parse the UniversalAccountId struct from the first result
	// go-ethereum ABI unpacker generates structs with json tags matching the ABI names
	raw, ok := results[0].(struct {
		ChainNamespace string `json:"chainNamespace"`
		ChainId        string `json:"chainId"`
		Owner          []byte `json:"owner"`
	})
	if !ok {
		return nil, true, fmt.Errorf("failed to cast UniversalAccountId from factory result, got type %T", results[0])
	}

	origin := &types.UniversalAccountId{
		ChainNamespace: raw.ChainNamespace,
		ChainId:        raw.ChainId,
		Owner:          fmt.Sprintf("0x%x", raw.Owner),
	}

	return origin, true, nil
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
		nil,
		true,  // commit = true (real tx, not simulation)
		false, // gasless = false (@dev: we need gas to be emitted in the tx receipt)
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

// Calls UniversalCore Contract to set chain metadata (gas price + chain height).
// The contract uses block.timestamp for the observed-at value.
func (k Keeper) CallUniversalCoreSetChainMeta(
	ctx sdk.Context,
	chainNamespace string,
	price *big.Int,
	chainHeight *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse Handler Contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress,
		handlerAddr,
		big.NewInt(0),
		nil,
		true,
		false,
		true,
		&nonce,
		"setChainMeta",
		chainNamespace,
		price,
		chainHeight,
	)
}

// GetUniversalCoreQuoterAddress reads the uniswapV3Quoter address stored in UniversalCore.
func (k Keeper) GetUniversalCoreQuoterAddress(ctx sdk.Context) (common.Address, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to parse UniversalCore ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	receipt, err := k.evmKeeper.CallEVM(ctx, abi, ueModuleAccAddress, handlerAddr, false, "uniswapV3Quoter")
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to call uniswapV3Quoter")
	}

	results, err := abi.Methods["uniswapV3Quoter"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to unpack uniswapV3Quoter result")
	}

	return results[0].(common.Address), nil
}

// GetUniversalCoreWPCAddress reads the WPC (wrapped PC) address stored in UniversalCore.
func (k Keeper) GetUniversalCoreWPCAddress(ctx sdk.Context) (common.Address, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to parse UniversalCore ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	receipt, err := k.evmKeeper.CallEVM(ctx, abi, ueModuleAccAddress, handlerAddr, false, "WPC")
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to call WPC")
	}

	results, err := abi.Methods["WPC"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return common.Address{}, errors.Wrap(err, "failed to unpack WPC result")
	}

	return results[0].(common.Address), nil
}

// GetDefaultFeeTierForToken reads defaultFeeTier[prc20] from UniversalCore.
func (k Keeper) GetDefaultFeeTierForToken(ctx sdk.Context, prc20Address common.Address) (*big.Int, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UniversalCore ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	receipt, err := k.evmKeeper.CallEVM(ctx, abi, ueModuleAccAddress, handlerAddr, false, "defaultFeeTier", prc20Address)
	if err != nil {
		return nil, errors.Wrap(err, "failed to call defaultFeeTier")
	}

	results, err := abi.Methods["defaultFeeTier"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unpack defaultFeeTier result")
	}

	// go-ethereum unpacks uint24 as *big.Int (non-standard widths always map to *big.Int)
	fee, ok := results[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected type for defaultFeeTier: %T", results[0])
	}

	return fee, nil
}

// GetSwapQuote calls QuoterV2.quoteExactInputSingle (commit=false) to get the expected
// output amount for swapping prc20 → wpc.
func (k Keeper) GetSwapQuote(
	ctx sdk.Context,
	quoterAddr, prc20Address, wpcAddress common.Address,
	fee, amount *big.Int,
) (*big.Int, error) {
	quoterABI, err := types.ParseUniswapQuoterV2ABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse QuoterV2 ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	params := types.AbiQuoteExactInputSingleParams{
		TokenIn:           prc20Address,
		TokenOut:          wpcAddress,
		AmountIn:          amount,
		Fee:               fee,
		SqrtPriceLimitX96: big.NewInt(0),
	}

	receipt, err := k.evmKeeper.CallEVM(ctx, quoterABI, ueModuleAccAddress, quoterAddr, false, "quoteExactInputSingle", params)
	if err != nil {
		return nil, errors.Wrap(err, "QuoterV2 quoteExactInputSingle failed")
	}

	results, err := quoterABI.Methods["quoteExactInputSingle"].Outputs.Unpack(receipt.Ret)
	if err != nil {
		return nil, errors.Wrap(err, "failed to unpack quoteExactInputSingle result")
	}

	amountOut, ok := results[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected type for amountOut: %T", results[0])
	}

	return amountOut, nil
}

// Calls Handler Contract to deposit prc20 tokens with auto-swap.
// fee and minPCOut must be pre-computed by the caller (see GetDefaultFeeTierForToken / GetSwapQuote).
func (k Keeper) CallPRC20DepositAutoSwap(
	ctx sdk.Context,
	prc20Address, to common.Address,
	amount, fee, minPCOut *big.Int,
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
		fee,
		minPCOut,
		big.NewInt(0), // deadline = 0 → contract uses its default
	)
}

// CallUniversalCoreRefundUnusedGas calls refundUnusedGas on UniversalCore to return excess gas fee
// to the recipient. withSwap=true swaps the gas token back to PC; withSwap=false deposits PRC20 directly.
func (k Keeper) CallUniversalCoreRefundUnusedGas(
	ctx sdk.Context,
	gasToken common.Address,
	amount *big.Int,
	recipient common.Address,
	withSwap bool,
	fee *big.Int,
	minPCOut *big.Int,
) (*evmtypes.MsgEthereumTxResponse, error) {
	handlerAddr := common.HexToAddress(uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CORE"].Address)

	abi, err := types.ParseUniversalCoreABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse UniversalCore ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}

	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	// fee is uint24 in Solidity — pass as *big.Int (go-ethereum ABI packs non-standard widths as *big.Int)
	return k.evmKeeper.DerivedEVMCall(
		ctx,
		abi,
		ueModuleAccAddress,
		handlerAddr,
		big.NewInt(0),
		nil,
		true,
		false,
		true,
		&nonce,
		"refundUnusedGas",
		gasToken,
		amount,
		recipient,
		withSwap,
		fee,
		minPCOut,
	)
}

// CallExecuteUniversalTx calls executeUniversalTx on a smart-contract recipient.
// This is used for isCEA inbounds whose recipient is a deployed contract (not a UEA).
func (k Keeper) CallExecuteUniversalTx(
	ctx sdk.Context,
	recipientAddr common.Address,
	sourceChain string,
	ceaAddress []byte,
	payload []byte,
	amount *big.Int,
	prc20AssetAddr common.Address,
	txId [32]byte,
) (*evmtypes.MsgEthereumTxResponse, error) {
	recipientABI, err := types.ParseRecipientContractABI()
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse recipient contract ABI")
	}

	ueModuleAccAddress, _ := k.GetUeModuleAddress(ctx)

	nonce, err := k.GetModuleAccountNonce(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := k.IncrementModuleAccountNonce(ctx); err != nil {
		return nil, err
	}

	return k.evmKeeper.DerivedEVMCall(
		ctx,
		recipientABI,
		ueModuleAccAddress,
		recipientAddr,
		big.NewInt(0),
		nil,
		true,
		false,
		true,
		&nonce,
		"executeUniversalTx",
		sourceChain,
		ceaAddress,
		payload,
		amount,
		prc20AssetAddr,
		txId,
	)
}

