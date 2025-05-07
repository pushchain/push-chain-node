package keeper

import (
	"context"
	"encoding/hex"
	"strings"

	"fmt"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"

	"cosmossdk.io/errors"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/rollchains/pchain/x/crosschain/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

func hexToBytes(hexStr string) ([]byte, error) {
	hexStr = strings.TrimPrefix(hexStr, "0x")
	return hex.DecodeString(hexStr)
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

	fmt.Println("Factory address:", factoryAddress)
	fmt.Println("Verifier precompile address:", verifierPrecompile)
	fmt.Println("User key:", userKey)
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
	// ctx := sdk.UnwrapSDKContext(goCtx)
	panic("MintPush is unimplemented")
	return &types.MsgMintPushResponse{}, nil
}
