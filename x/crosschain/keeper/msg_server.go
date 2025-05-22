package keeper

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cosmossdk.io/errors"
	sdkmath "cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkErrors "github.com/cosmos/cosmos-sdk/types/errors"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/ethereum/go-ethereum/common"
	pchaintypes "github.com/push-protocol/push-chain/types"
	"github.com/push-protocol/push-chain/util"
	"github.com/push-protocol/push-chain/x/crosschain/types"
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

	// Construct CAIP address from AccountId for transaction verification
	if msg.AccountId == nil {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "account ID is required")
	}

	// Format: namespace:chainId:ownerKey (e.g., eip155:1:0x123...)
	caipAddress := fmt.Sprintf("%s:%s:%s",
		msg.AccountId.Namespace,
		msg.AccountId.ChainId,
		msg.AccountId.OwnerKey)

	// Verify the transaction on the source chain using the USVL module
	// Ensure the transaction is directed to the locker contract
	verificationResult, err := ms.k.usvlKeeper.VerifyExternalTransactionToLocker(ctx, msg.TxHash, caipAddress)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to verify transaction %s for CAIP address %s", msg.TxHash, caipAddress)
	}

	// Check if the transaction was verified successfully
	if !verificationResult.Verified {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest,
			"transaction verification failed: %s", verificationResult.TxInfo)
	}

	// Log that transaction verification was successful
	ms.k.logger.Info("Transaction verified successfully as directed to locker contract",
		"txHash", msg.TxHash,
		"caipAddress", caipAddress,
		"txInfo", verificationResult.TxInfo)

	// Retrieve the current Params
	adminParams, err := ms.k.AdminParams.Get(ctx)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get admin params")
	}

	_, evmFromAddress, err := util.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	// EVM Call arguments
	factoryAddress := common.HexToAddress(adminParams.FactoryAddress)
	accountId, err := types.NewAbiAccountId(msg.AccountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create accountId")
	}

	// Use your keeper CallEVM directly
	receipt, err := ms.k.CallFactoryToDeployNMSC(
		sdkCtx,
		evmFromAddress,
		factoryAddress,
		accountId,
	)
	if err != nil {
		return nil, err
	}

	fmt.Println("DeployNMSC receipt:", receipt)
	returnedBytesHex := common.Bytes2Hex(receipt.Ret)
	fmt.Println("Returned Bytes Hex:", returnedBytesHex)

	return &types.MsgDeployNMSCResponse{
		SmartAccount: receipt.Ret,
	}, nil
}

// MintPush handles token minting to the user's NMSC for the tokens locked on source chain.
func (ms msgServer) MintPush(ctx context.Context, msg *types.MsgMintPush) (*types.MsgMintPushResponse, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)

	// Construct CAIP address from AccountId for transaction verification
	if msg.AccountId == nil {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "account ID is required")
	}

	// Format: namespace:chainId:ownerKey (e.g., eip155:1:0x123...)
	caipAddress := fmt.Sprintf("%s:%s:%s",
		msg.AccountId.Namespace,
		msg.AccountId.ChainId,
		msg.AccountId.OwnerKey)

	// Verify the transaction on the source chain using the USVL module
	// Ensure the transaction is directed to the locker contract
	verificationResult, err := ms.k.usvlKeeper.VerifyExternalTransactionToLocker(ctx, msg.TxHash, caipAddress)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to verify transaction %s for CAIP address %s", msg.TxHash, caipAddress)
	}

	// Check if the transaction was verified successfully
	if !verificationResult.Verified {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest,
			"transaction verification failed: %s", verificationResult.TxInfo)
	}

	// Log that transaction verification was successful
	ms.k.logger.Info("Transaction verified successfully as directed to locker contract",
		"txHash", msg.TxHash,
		"caipAddress", caipAddress,
		"txInfo", verificationResult.TxInfo)

	// Parse CAIP address to get the chain identifier (e.g., "eip155:11155111")
	parts := strings.Split(caipAddress, ":")
	if len(parts) < 2 {
		return nil, errors.Wrapf(sdkErrors.ErrInvalidRequest, "invalid CAIP address format: %s", caipAddress)
	}
	chainIdentifier := parts[0] + ":" + parts[1]

	// Extract the token amount from the FundsAdded event in the transaction logs
	// The FundsAdded event has signature: FundsAdded(msg.sender, usdtReceived, _transactionHash)
	// Get the event topic signature for this chain
	fundsAddedTopic, err := ms.k.usvlKeeper.GetFundsAddedEventTopic(ctx, chainIdentifier)
	if err != nil {
		ms.k.logger.Error("Failed to get FundsAddedEventTopic, using default",
			"chainIdentifier", chainIdentifier,
			"error", err.Error())
		// Fall back to the default topic signature if there was an error
		fundsAddedTopic = "0xddcd6ef7998ae51b4ead4e9aa669a7d5ff30af88eddaa5062c91b08153da07c0"
	}
	// Get the VM type from the chain identifier (for future support of different VM types)
	// For now, we assume it's EVM since that's the only implementation
	vmType := uint8(1) // 1 = VmTypeEvm
	
	amountToMint, err := ExtractAmountFromTransactionLogs(verificationResult.TxInfo, fundsAddedTopic, vmType)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to extract token amount from transaction logs")
	}

	// If amount is zero or not found, fallback to a default amount (should not happen in production)
	if amountToMint.IsZero() {
		ms.k.logger.Error("Failed to extract valid amount from transaction logs, using fallback amount",
			"txHash", msg.TxHash)
		amountToMint = sdkmath.NewInt(1000000000000000000) // 1 token as fallback
	}

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
	accountId, err := types.NewAbiAccountId(msg.AccountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create accountId")
	}

	// Calling factory contract to compute the smart account address
	receipt, err := ms.k.CallFactoryToComputeAddress(sdkCtx, evmFromAddress, factoryAddress, accountId)
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
	accountId, err := types.NewAbiAccountId(msg.AccountId)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to create accountId")
	}

	_, evmFromAddress, err := util.GetAddressPair(msg.Signer)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to parse signer address")
	}

	// Step 2: Compute smart account address
	receipt, err := ms.k.CallFactoryToComputeAddress(sdkCtx, evmFromAddress, factoryAddress, accountId)
	if err != nil {
		return nil, err
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

// ExtractAmountFromTransactionLogs extracts the token amount from transaction logs
// based on a specific event topic signature. It supports different VMs by using
// the vmType parameter to determine how to parse the logs.
func ExtractAmountFromTransactionLogs(txInfoJSON string, eventTopicSignature string, vmType uint8) (sdkmath.Int, error) {
	// Parse the transaction info JSON
	var txInfo struct {
		Logs json.RawMessage `json:"logs"`
	}
	if err := json.Unmarshal([]byte(txInfoJSON), &txInfo); err != nil {
		return sdkmath.ZeroInt(), fmt.Errorf("failed to parse transaction info: %w", err)
	}

	// If logs is null or empty array JSON, return error
	if string(txInfo.Logs) == "null" || string(txInfo.Logs) == "[]" {
		return sdkmath.ZeroInt(), fmt.Errorf("no logs found in transaction")
	}

	// Extract based on VM type
	switch vmType {
	case 1: // VmTypeEvm
		return ExtractAmountFromEVMLogs(txInfo.Logs, eventTopicSignature)
	case 2: // VmTypeSvm (Solana)
		// For future implementation
		return sdkmath.ZeroInt(), fmt.Errorf("Solana VM log extraction not yet implemented")
	case 3: // VmTypeWasm
		// For future implementation
		return sdkmath.ZeroInt(), fmt.Errorf("WASM VM log extraction not yet implemented")
	default:
		// Default to EVM for backward compatibility
		return ExtractAmountFromEVMLogs(txInfo.Logs, eventTopicSignature)
	}
}

// ExtractAmountFromEVMLogs extracts amount from EVM-formatted logs
func ExtractAmountFromEVMLogs(logsJSON json.RawMessage, eventTopicSignature string) (sdkmath.Int, error) {
	var logs []struct {
		Address string   `json:"address"`
		Topics  []string `json:"topics"`
		Data    string   `json:"data"`
	}
	if err := json.Unmarshal(logsJSON, &logs); err != nil {
		return sdkmath.ZeroInt(), fmt.Errorf("failed to parse EVM transaction logs: %w", err)
	}

	// Check if logs array is empty
	if len(logs) == 0 {
		return sdkmath.ZeroInt(), fmt.Errorf("no logs found in transaction")
	}

	// Look for the specific event (FundsAdded) based on topic signature
	for _, log := range logs {
		if len(log.Topics) > 0 && log.Topics[0] == eventTopicSignature {
			// First try to extract amount from topics (indexed parameter case)
			// For event: FundsAdded(address indexed sender, uint256 indexed usdtReceived, bytes32 _transactionHash)
			if len(log.Topics) >= 3 {
				// Check if the third topic contains the amount (typical for indexed param)
				amountTopic := log.Topics[2]
				// Remove "0x" prefix if present
				if strings.HasPrefix(amountTopic, "0x") {
					amountTopic = amountTopic[2:]
				}
				
				// Try parsing as hex by adding 0x prefix
				bigInt, success := sdkmath.NewIntFromString("0x" + amountTopic)
				if success {
					return bigInt, nil
				}
			}

			// If no amount in topics, check data field for non-indexed parameters
			// For event: FundsAdded(address indexed sender, uint256 usdtReceived, bytes32 _transactionHash)
			if log.Data != "" {
				// Remove "0x" prefix if present
				data := log.Data
				if strings.HasPrefix(data, "0x") {
					data = data[2:]
				}

				// In EVM logs, parameters are 32 bytes each (64 hex chars)
				if len(data) >= 64 { // At least 1 parameter
					// First try extracting the first 32 bytes (most common for amount in our case)
					amountHex := data[0:64]
					// Try parsing as hex
					bigInt, success := sdkmath.NewIntFromString("0x" + amountHex)
					if success {
						return bigInt, nil
					}
					
					// If the first parameter doesn't work, try the second parameter
					// (some contracts might have a different layout)
					if len(data) >= 128 {
						amountHex = data[64:128]
						bigInt, success = sdkmath.NewIntFromString("0x" + amountHex)
						if success {
							return bigInt, nil
						}
					}
				}
			}
		}
	}

	return sdkmath.ZeroInt(), fmt.Errorf("FundsAdded event or amount parameter not found in EVM logs")
}
