package keeper

import (
	"context"
	"math"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/rollchains/pchain/types"
	"github.com/rollchains/pchain/util"
	"github.com/rollchains/pchain/x/crosschain/types"
)

type msgServer struct {
	k Keeper
}

var _ types.MsgServer = msgServer{}

// NewMsgServerImpl returns an implementation of the module MsgServer interface
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{k: keeper}
}

// UpdateParams handles MsgUpdateParams for updating module parameters.
// Only authorized governance account can execute this.
func (ms msgServer) UpdateParams(ctx context.Context, msg *types.MsgUpdateParams) (*types.MsgUpdateParamsResponse, error) {
	if ms.k.authority != msg.Authority {
		return nil, errors.Wrapf(govtypes.ErrInvalidSigner, "invalid authority; expected %s, got %s", ms.k.authority, msg.Authority)
	}

	return nil, ms.k.Params.Set(ctx, msg.Params)
}

// UpdateAdminParams handles updates to admin parameters.
// Only current admin can execute this.
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

// DeployNMSC handles the deployment of new Smart Account (NMSC).
func (ms msgServer) DeployNMSC(ctx context.Context, msg *types.MsgDeployNMSC) (*types.MsgDeployNMSCResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// TODO: RPC call to verify if user has locked funds on source chain

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	_, evmFromAddress, err := util.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	if msg.OwnerType > math.MaxUint8 {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "ownerType must be a uint8 integer, got %d", msg.OwnerType)
	}

	// EVM Call arguments
	userKey, err := util.HexToBytes(msg.UserKey)
	if err != nil {
		return nil, err
	}

	caipString := msg.CaipString
	ownerType := uint8(msg.OwnerType)
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)
	verifierPrecompile := common.HexToAddress(adminParams.VerifierPrecompile)

	// Use your keeper CallEVM directly
	receipt, err := ms.k.CallFactoryToDeployNMSC(
		sdkCtx,
		evmFromAddress,
		factoryAddress,
		verifierPrecompile,
		userKey,
		caipString,
		ownerType,
	)
	if err != nil {
		return nil, err
	}

	return &types.MsgDeployNMSCResponse{
		SmartAccount: receipt.Ret,
	}, nil
}

// MintPush handles token minting to the user's NMSC for the tokens locked on source chain.
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
	amountToMint := sdkmath.NewInt(1000000000000000000) // 1 token

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	_, evmFromAddress, err := util.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)
	if factoryAddress == common.HexToAddress("0x0") {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidAddress, "invalid factory address")
	}

	// Calling factory contract to compute the smart account address
	receipt, err := ms.k.CallFactoryToComputeAddress(sdkCtx, evmFromAddress, factoryAddress, msg.CaipString)
	if err != nil {
		return nil, err
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	nmscComputedAddress := "0x" + addressBytes

	// Convert the computed address to a Cosmos address
	cosmosAddr, err := util.ConvertAnyAddressToBytes(nmscComputedAddress)

	if err != nil {
		return nil, errors.Wrapf(err, "failed to convert EVM address to Cosmos address")
	}

	err = ms.k.bankKeeper.MintCoins(ctx, types.ModuleName, sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint)))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to mint coins")
	}

	err = ms.k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, cosmosAddr, sdk.NewCoins(sdk.NewCoin(pchaintypes.BaseDenom, amountToMint)))
	if err != nil {
		return nil, errors.Wrapf(err, "failed to send coins from module to account")
	}

	return &types.MsgMintPushResponse{}, nil
}

// ExecutePayload handles cross-chain payload execution on the NMSC.
func (ms msgServer) ExecutePayload(ctx context.Context, msg *types.MsgExecutePayload) (*types.MsgExecutePayloadResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Step 1: Get params and validate addresses
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)
	if factoryAddress == common.HexToAddress("0x0") {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidAddress, "invalid factory address")
	}

	_, evmFromAddress, err := util.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	// Step 2: Compute smart account address
	receipt, err := ms.k.CallFactoryToComputeAddress(sdkCtx, evmFromAddress, factoryAddress, msg.CaipString)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to compute smart account for CAIP: %s", msg.CaipString)
	}

	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	addressBytes := returnedBytesHex[24:] // last 20 bytes
	nmscComputedAddress := "0x" + addressBytes
	nmscAddr := common.HexToAddress(nmscComputedAddress)

	// Step 3: Parse and validate payload and signature
	payload, err := types.NewAbiCrossChainPayload(msg.CrosschainPayload)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid cross-chain payload")
	}

	signatureVal, err := util.HexToBytes(msg.Signature)
	if err != nil {
		return nil, errors.Wrapf(err, "invalid signature format")
	}

	// Step 4: Execute payload through NMSC
	receipt, err = ms.k.CallNMSCExecutePayload(sdkCtx, evmFromAddress, nmscAddr, payload, signatureVal)
	if err != nil {
		return nil, err
	}

	// Step 5: Handle fee calculation and deduction
	nmscAccAddr := sdk.AccAddress(nmscAddr.Bytes())

	baseFee := ms.k.feemarketKeeper.GetBaseFee(sdkCtx)
	if baseFee.IsNil() {
		return nil, errors.Wrapf(sdkErrors.ErrLogic, "base fee not found")
	}

	gasCost, err := ms.k.CalculateGasCost(baseFee, payload.MaxFeePerGas, payload.MaxPriorityFeePerGas, receipt.GasUsed)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to calculate gas cost")
	}

	if gasCost.Cmp(payload.GasLimit) > 0 {
		return nil, errors.Wrapf(sdkErrors.ErrOutOfGas, "gas cost (%d) exceeds limit (%d)", gasCost, payload.GasLimit)
	}

	if err = ms.k.DeductAndBurnFees(ctx, nmscAccAddr, gasCost); err != nil {
		return nil, errors.Wrapf(err, "failed to deduct fees from %s", nmscAccAddr)
	}

	return &types.MsgExecutePayloadResponse{}, nil
}
