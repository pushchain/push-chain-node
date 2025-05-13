package keeper

import (
	"context"
	"encoding/hex"
	"math/big"
	"strings"

	"fmt"
	"math"

	sdkmath "cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	pushtypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/x/crosschain/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

func hexToBytes(hexStr string) ([]byte, error) {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	fmt.Println("Hex string:", hexStr)
	fmt.Println(hex.DecodeString(hexStr))
	return hex.DecodeString(hexStr)
}

func stringToBigInt(s string) *big.Int {
	bi, _ := new(big.Int).SetString(s, 10)
	return bi
}

func evmToCosmosAddress(evmAddr string) (sdk.AccAddress, error) {
	if len(evmAddr) != 42 {
		return nil, fmt.Errorf("invalid EVM address length")
	}

	// Decode the hex address (without 0x)
	addrBytes, err := hex.DecodeString(evmAddr[2:])
	if err != nil {
		return nil, err
	}

	// Convert to cosmos bech32 address
	cosmosAddr := sdk.AccAddress(addrBytes)
	return cosmosAddr, nil
}

// NewMsgServerImpl returns an implementation of the module MsgServer interface.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.k.authority, msg.Authority)
	}

	return nil, ms.k.Params.Set(ctx, msg.Params)
}

// UpdateAdminParams implements types.MsgServer.
func (ms msgServer) UpdateAdminParams(ctx context.Context, msg *types.MsgUpdateAdminParams) (*types.MsgUpdateAdminParamsResponse, error) {
	// Retrieve the current Params
	params, err := ms.k.Params.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get params")
	}

	// Check if the sender is the admin (from params)
	if params.Admin != msg.Admin {
		return nil, errors.Wrapf(sdkErrors.ErrUnauthorized, "invalid admin; expected admin address %s, got %s", params.Admin, msg.Admin)
	}

	return nil, ms.k.AdminParams.Set(ctx, msg.AdminParams)
}

// DeployNMSC implements types.MsgServer.
func (ms msgServer) DeployNMSC(ctx context.Context, msg *types.MsgDeployNMSC) (*types.MsgDeployNMSCResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	fmt.Println("Admin params:", adminParams)
	fmt.Println(msg.UserKey)
	fmt.Println(msg.CaipString)
	fmt.Println(msg.OwnerType)
	fmt.Println(msg.Signer)

	// Get the Cosmos address in Bech32 format (from the signer in MsgDeployNMSC)
	signer := msg.Signer

	// Convert the Bech32 address to sdk.AccAddress
	cosmosAddr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert Bech32 address")
	}

	// Now convert the Cosmos address to an EVM address
	evmFromAddress := common.BytesToAddress(cosmosAddr.Bytes()[len(cosmosAddr)-20:])

	// Parse ABI once
	parsedABI, err := abi.JSON(strings.NewReader(types.FactoryV1ABI))
	if err != nil {
		return nil, err
	}

	if msg.OwnerType > math.MaxUint8 {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "ownerType must be a uint8 integer, got %d", msg.OwnerType)
	}

	// EVM Call arguments
	userKey, err := hexToBytes(msg.UserKey)
	if err != nil {
		return nil, err
	}

	caipString := msg.CaipString
	ownerType := uint8(msg.OwnerType)
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)
	verifierPrecompile := common.HexToAddress(adminParams.VerifierPrecompile)

	userKeys := common.HexToAddress("0x778D3206374f8AC265728E18E3fE2Ae6b93E4ce4").Bytes()
	fmt.Println("User key:", userKeys)

	fmt.Println("Factory address:", factoryAddress)
	fmt.Println("Verifier precompile address:", verifierPrecompile)
	fmt.Println("User key:", userKey)
	fmt.Println(msg.UserKey)
	fmt.Println("CAIP string:", caipString)
	fmt.Println("Owner type:", ownerType)
	fmt.Println("EVM from address:", evmFromAddress)
	fmt.Println(ms.k.evmKeeper)

	// Use your keeper CallEVM directly
	receipt, err := ms.k.evmKeeper.CallEVM(
		sdkCtx,
		parsedABI,
		evmFromAddress, // who is sending the transaction
		factoryAddress, // destination: your FactoryV1 contract
		true,           // commit = true (you want real tx, not simulation)
		"deploySmartAccount",
		userKey,
		caipString,
		ownerType,
		verifierPrecompile,
	)

	if err != nil {
		return nil, err
	}

	fmt.Println("EVM tx hash:", receipt.Hash)
	fmt.Println("Gas used:", receipt.GasUsed)
	fmt.Println("Logs:", receipt.Logs)
	fmt.Println("Returned data:", receipt.Ret)
	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}
	fmt.Println("Return data:", common.Bytes2Hex(receipt.Ret))

	return &types.MsgDeployNMSCResponse{
		SmartAccount: receipt.Ret,
	}, nil
}

// MintPush implements types.MsgServer.
func (ms msgServer) MintPush(ctx context.Context, msg *types.MsgMintPush) (*types.MsgMintPushResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// caipArr := strings.Split(msg.CaipString, ":")
	// if len(caipArr) != 3 {
	// 	return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "invalid CAIP string; expected format: <namespace>:<chain>:<address>, got %s", msg.CaipString)
	// }

	// userAddr := caipArr[2]
	// txHash := msg.TxHash
	// TODO
	// 1. RPC call for verification
	amountToMint := sdkmath.NewInt(1000) // 1 token

	// nmscAddress := calculateKeccakSalt(msg.CaipString)

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	// Get the Cosmos address in Bech32 format (from the signer in MsgDeployNMSC)
	signer := msg.Signer

	// Convert the Bech32 address to sdk.AccAddress
	cosmosAddr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert Bech32 address")
	}

	// Now convert the Cosmos address to an EVM address
	evmFromAddress := common.BytesToAddress(cosmosAddr.Bytes()[len(cosmosAddr)-20:])
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)

	// Parse ABI once
	parsedABI, err := abi.JSON(strings.NewReader(types.FactoryV1ABI))
	if err != nil {
		return nil, err
	}

	// Use your keeper CallEVM directly
	receipt, err := ms.k.evmKeeper.CallEVM(
		sdkCtx,
		parsedABI,
		evmFromAddress, // who is sending the transaction
		factoryAddress, // destination: your FactoryV1 contract
		false,          // commit = true (you want real tx, not simulation)
		"computeSmartAccountAddress",
		msg.CaipString,
	)
	if err != nil {
		return nil, err
	}

	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	nmscComputedAddress := "0x" + addressBytes

	// Convert the computed address to a Cosmos address
	cosmosAddr, err = evmToCosmosAddress(nmscComputedAddress)

	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert EVM address to Cosmos address")
	}

	err = ms.k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(pushtypes.BaseDenom, amountToMint)))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to mint coins")
	}

	err = ms.k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, cosmosAddr, sdk.NewCoins(sdk.NewCoin(pushtypes.BaseDenom, amountToMint)))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to send coins from module to account")
	}

	return &types.MsgMintPushResponse{}, nil
}

// ExecutePayload implements types.MsgServer.
func (ms msgServer) ExecutePayload(ctx context.Context, msg *types.MsgExecutePayload) (*types.MsgExecutePayloadResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	// Get the Cosmos address in Bech32 format (from the signer in MsgDeployNMSC)
	signer := msg.Signer

	// Convert the Bech32 address to sdk.AccAddress
	cosmosAddr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert Bech32 address")
	}

	// Now convert the Cosmos address to an EVM address
	evmFromAddress := common.BytesToAddress(cosmosAddr.Bytes()[len(cosmosAddr)-20:])
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)

	// Parse ABI once
	parsedFactoryABI, err := abi.JSON(strings.NewReader(types.FactoryV1ABI))
	if err != nil {
		return nil, err
	}

	// Calling factory contract to compute the smart account address
	receipt, err := ms.k.evmKeeper.CallEVM(
		sdkCtx,
		parsedFactoryABI,
		evmFromAddress, // who is sending the transaction
		factoryAddress, // destination: FactoryV1 contract
		false,          // commit = false
		"computeSmartAccountAddress",
		msg.CaipString,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("EVM tx hash:", receipt.Hash)
	fmt.Println("Gas used:", receipt.GasUsed)
	fmt.Println("Logs:", receipt.Logs)
	fmt.Println("Returned data:", receipt.Ret)
	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}
	fmt.Println("Return data:", common.Bytes2Hex(receipt.Ret))

	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	nmscComputedAddress := "0x" + addressBytes
	nmscAddr := common.HexToAddress(nmscComputedAddress)

	parsedNMSCABI, err := abi.JSON(strings.NewReader(types.SmartAccountV1ABI))
	if err != nil {
		return nil, err
	}

	fmt.Println(parsedNMSCABI)

	protoPayload := msg.CrosschainPayload // your existing proto-generated struct
	dataVal, err := hexToBytes(protoPayload.Data)
	if err != nil {
		return nil, err
	}

	payload := types.AbiCrossChainPayload{
		Target:               common.HexToAddress(protoPayload.Target),
		Value:                stringToBigInt(protoPayload.Value),
		Data:                 dataVal,
		GasLimit:             stringToBigInt(protoPayload.GasLimit),
		MaxFeePerGas:         stringToBigInt(protoPayload.MaxFeePerGas),
		MaxPriorityFeePerGas: stringToBigInt(protoPayload.MaxPriorityFeePerGas),
		Nonce:                stringToBigInt(protoPayload.Nonce),
		Deadline:             stringToBigInt(protoPayload.Deadline),
	}

	signatureVal, err := hexToBytes(msg.Signature)
	if err != nil {
		return nil, err
	}

	// Calling the NMSC contract
	receipt, err = ms.k.evmKeeper.CallEVM(
		sdkCtx,
		parsedNMSCABI,
		evmFromAddress, // who is sending the transaction
		nmscAddr,       // destination: nmsc contract
		true,           // commit = true
		"executePayload",
		payload,
		signatureVal,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("EVM tx hash:", receipt.Hash)
	fmt.Println("Gas used:", receipt.GasUsed)
	fmt.Println("Logs:", receipt.Logs)
	fmt.Println("Returned data:", receipt.Ret)
	if receipt.VmError != "" {
		fmt.Println("VM Error:", receipt.VmError)
	}
	fmt.Println("Return data:", common.Bytes2Hex(receipt.Ret))

	// Deduct fee from targetAddr
	nmscAccAddr := sdk.AccAddress(nmscAddr.Bytes())
	burnAmt := sdkmath.NewInt(int64(receipt.GasUsed))     // ← set based on your gasUsed * gasPrice logic
	burnCoin := sdk.NewCoin(pushtypes.BaseDenom, burnAmt) // or your chain's denom

	err = ms.k.bankKeeper.SendCoinsFromAccountToModule(
		ctx,
		nmscAccAddr,
		types.ModuleName, // or any module account name
		sdk.NewCoins(burnCoin),
	)
	if err != nil {
		return nil, err
	}
	fmt.Println("Received tokens from nmsc addr in module")

	// Burn from module account
	err = ms.k.bankKeeper.BurnCoins(
		ctx,
		types.ModuleName,
		sdk.NewCoins(burnCoin),
	)
	if err != nil {
		return nil, err
	}
	fmt.Println("Burned tokens from module account")

	return &types.MsgExecutePayloadResponse{}, nil
}
