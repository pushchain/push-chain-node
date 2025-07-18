package keeper

import (
	"context"
	"fmt"

	"github.com/rollchains/pchain/utils"
	"github.com/rollchains/pchain/utils/rpc"
	evmrpc "github.com/rollchains/pchain/utils/rpc/evm"
	uetypes "github.com/rollchains/pchain/x/ue/types"
	utvtypes "github.com/rollchains/pchain/x/utv/types"
)

// verifyEVMInteraction verifies user interacted with gateway by checking tx sent by ownerKey to gateway contract
func (k Keeper) verifyEVMInteraction(ctx context.Context, ownerKey, txHash string, chainConfig uetypes.ChainConfig) error {
	_, err := k.VerifyEVMInboundTx(ctx, ownerKey, txHash, chainConfig)
	if err != nil {
		return err
	}

	return nil
}

// verifyEVMAndGetPayload verifies and extracts payloadHash sent by the user in the tx
func (k Keeper) verifyEVMAndGetPayload(ctx context.Context, ownerKey, txHash string, chainConfig uetypes.ChainConfig) (string, error) {
	metadata, err := k.VerifyEVMInboundTx(ctx, ownerKey, txHash, chainConfig)
	if err != nil {
		return "", err
	}

	return metadata.PayloadHash, err
}

// Verifies and extracts locked amount (used in mint)
func (k Keeper) verifyEVMAndGetFunds(ctx context.Context, ownerKey, txHash string, chainConfig uetypes.ChainConfig) (*utvtypes.USDValue, error) {
	// Fetch stored metadata
	metadata, err := k.VerifyEVMInboundTx(ctx, ownerKey, txHash, chainConfig)
	if err != nil {
		return nil, err
	}

	// Check if already minted
	if metadata.Minted {
		return nil, fmt.Errorf("tokens already minted for txHash %s on chain %s", txHash, chainConfig.Chain)
	}

	rpcCfg := rpc.RpcCallConfig{
		PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
		PublicRPC:  chainConfig.PublicRpcUrl,
	}

	// Check for valid block confirmations
	err = CheckEVMBlockConfirmations(ctx, txHash, rpcCfg, chainConfig.BlockConfirmation)
	if err != nil {
		return nil, err
	}

	// Mutate the original metadata
	metadata.Minted = true

	// Step 4: Mutate Minted to true in the stored metadata
	err = k.StoreVerifiedInboundTx(ctx, chainConfig.Chain, txHash, *metadata)
	if err != nil {
		return nil, err
	}

	return metadata.UsdValue, nil
}

func (k Keeper) VerifyEVMInboundTx(
	ctx context.Context,
	ownerKey, txHash string,
	chainConfig uetypes.ChainConfig,
) (*utvtypes.VerifiedTxMetadata, error) {
	meta, found, err := k.GetVerifiedInboundTxMetadata(ctx, chainConfig.Chain, txHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	if found {
		ok := compareEVMAddr(meta.Sender, ownerKey)
		if !ok {
			return nil, fmt.Errorf("ownerKey and sender of the tx mismatched: expected %s, got %s", meta.Sender, ownerKey)
		}
		return meta, nil
	}

	// If not found, perform verification
	return k.EVMProcessUnverifiedInboundTx(ctx, ownerKey, txHash, chainConfig)
}

func (k Keeper) EVMProcessUnverifiedInboundTx(
	ctx context.Context,
	ownerKey, txHash string,
	chainConfig uetypes.ChainConfig,
) (*utvtypes.VerifiedTxMetadata, error) {
	rpcCfg := rpc.RpcCallConfig{
		PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
		PublicRPC:  chainConfig.PublicRpcUrl,
	}

	// Step 1: Fetch transaction receipt
	receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcCfg, txHash)
	if err != nil {
		return nil, fmt.Errorf("fetch receipt failed: %w", err)
	}

	// Step 2: Fetch transaction details
	tx, err := evmrpc.EVMGetTransactionByHash(ctx, rpcCfg, txHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Normalize addresses for comparison
	from := NormalizeEVMAddress(receipt.From)
	to := NormalizeEVMAddress(receipt.To)
	expectedFrom := NormalizeEVMAddress(ownerKey)
	expectedTo := NormalizeEVMAddress(chainConfig.GatewayAddress)

	// INPUT CHECKS
	// Check 1: Verify if ownerKey is Valid From address
	if !compareEVMAddr(from, expectedFrom) {
		return nil, fmt.Errorf("transaction sender %s does not match ownerKey %s", tx.From, expectedFrom)
	}

	// Check 2: Verify if tx.To is Valid gateway address
	if !isValidEVMGateway(to, expectedTo) {
		return nil, fmt.Errorf("transaction recipient %s is not gateway address %s", tx.To, expectedTo)
	}

	// Check 3: Verify if transaction is calling addFunds method
	ok, selector := isEVMTxCallingAddFunds(tx.Input, chainConfig)
	if !ok {
		return nil, fmt.Errorf("transaction is not calling addFunds, expected selector %s but got input %s", selector, tx.Input)
	}

	// Step 3: Extract values from logs
	eventTopic := ""
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == uetypes.METHOD.EVM.AddFunds {
			eventTopic = method.EventIdentifier
			break
		}
	}
	if eventTopic == "" {
		return nil, fmt.Errorf("addFunds method not found in gateway methods")
	}

	fundsAddedEventLogs, err := ParseEVMFundsAddedEventLogs(receipt.Logs, eventTopic)
	if err != nil {
		return nil, fmt.Errorf("amount extract failed: %w", err)
	}

	metadata := utvtypes.VerifiedTxMetadata{
		Minted:      false,
		PayloadHash: fundsAddedEventLogs.PayloadHash,
		UsdValue: &utvtypes.USDValue{
			Amount:   fundsAddedEventLogs.AmountInUSD.String(),
			Decimals: fundsAddedEventLogs.Decimals,
		},
		Sender: ownerKey,
	}

	// Step 4: Store verified inbound tx in storage
	err = k.StoreVerifiedInboundTx(ctx, chainConfig.Chain, txHash, metadata)
	if err != nil {
		return nil, err
	}

	// Step 5: Return the metadata
	return &metadata, nil
}
