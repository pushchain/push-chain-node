package keeper

import (
	"context"
	"encoding/hex"
	"fmt"

	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/utils/rpc"
	svmrpc "github.com/pushchain/push-chain-node/utils/rpc/svm"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utxverifiertypes "github.com/pushchain/push-chain-node/x/utxverifier/types"
)

// verifySVMInteraction verifies user interacted with gateway by checking tx sent by ownerKey to gateway contract
func (k Keeper) verifySVMInteraction(ctx context.Context, ownerKey, txHash string, chainConfig uregistrytypes.ChainConfig) error {
	_, err := k.VerifySVMInboundTx(ctx, ownerKey, txHash, chainConfig)
	if err != nil {
		return err
	}

	return nil
}

// verifyEVMAndGetPayload verifies and extracts payloadHash sent by the user in the tx
func (k Keeper) verifySVMAndGetPayload(ctx context.Context, ownerKey, txHash string, chainConfig uregistrytypes.ChainConfig) (string, error) {
	metadata, err := k.VerifySVMInboundTx(ctx, ownerKey, txHash, chainConfig)
	if err != nil {
		return "", err
	}

	return metadata.PayloadHash, err
}

// verifySVMAndGetFunds verifies transaction and extracts locked amount
func (k Keeper) verifySVMAndGetFunds(ctx context.Context, ownerKey, txHash string, chainConfig uregistrytypes.ChainConfig) (*utxverifiertypes.USDValue, error) {
	// Fetch stored metadata
	metadata, err := k.VerifySVMInboundTx(ctx, ownerKey, txHash, chainConfig)
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

	txHashBase58, err := utxverifiertypes.NormalizeTxHash(txHash, chainConfig.VmType)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize tx hash: %w", err)
	}

	// Check for valid block confirmations
	err = CheckSVMBlockConfirmations(ctx, txHashBase58, rpcCfg, uint64(chainConfig.BlockConfirmation.FastInbound))
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

func (k Keeper) VerifySVMInboundTx(
	ctx context.Context,
	ownerKey, txHash string,
	chainConfig uregistrytypes.ChainConfig,
) (*utxverifiertypes.VerifiedTxMetadata, error) {
	meta, found, err := k.GetVerifiedInboundTxMetadata(ctx, chainConfig.Chain, txHash)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch metadata: %w", err)
	}

	if found {
		ok := compareSVMAddresses(meta.Sender, ownerKey)
		if !ok {
			return nil, fmt.Errorf("ownerKey and sender of the tx mismatched: expected %s, got %s", meta.Sender, ownerKey)
		}
		return meta, nil
	}

	// If not found, perform verification
	return k.SVMProcessUnverifiedInboundTx(ctx, ownerKey, txHash, chainConfig)
}

func (k Keeper) SVMProcessUnverifiedInboundTx(
	ctx context.Context,
	ownerKey, txHash string,
	chainConfig uregistrytypes.ChainConfig,
) (*utxverifiertypes.VerifiedTxMetadata, error) {
	rpcCfg := rpc.RpcCallConfig{
		PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
		PublicRPC:  chainConfig.PublicRpcUrl,
	}

	txHashBase58, err := utxverifiertypes.NormalizeTxHash(txHash, chainConfig.VmType)
	if err != nil {
		return nil, fmt.Errorf("failed to normalize tx hash: %w", err)
	}

	// Step 1: Fetch transaction receipt
	tx, err := svmrpc.SVMGetTransactionBySig(ctx, rpcCfg, txHashBase58)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch transaction: %w", err)
	}

	// Verify transaction status
	if tx.Meta.Err != nil {
		return nil, fmt.Errorf("transaction failed with error: %v", tx.Meta.Err)
	}

	// Check 1: Check 1: Verify if ownerKey is Valid sender address
	_, err = IsValidSVMSender(tx.Transaction.Message.AccountKeys, ownerKey)
	if err != nil {
		return nil, err
	}

	// Verify program ID
	if len(tx.Transaction.Message.Instructions) == 0 {
		return nil, fmt.Errorf("no instructions found in transaction")
	}

	// Check2: Check if any instruction calls the gateway contract
	// err = IsValidSVMAddFundsInstruction(tx.Transaction.Message.Instructions, tx.Transaction.Message.AccountKeys, chainConfig)
	// if err != nil {
	// 	return nil, err
	// }

	// Step 3: Parse logs for FundsAddedEvent
	// Get the event discriminator from chain config
	var eventDiscriminator []byte
	for _, method := range chainConfig.GatewayMethods {
		if method.Name == uregistrytypes.GATEWAY_METHOD.SVM.AddFunds {
			eventDiscriminator, err = hex.DecodeString(method.EventIdentifier)
			if err != nil {
				return nil, fmt.Errorf("invalid event discriminator in chain config: %w", err)
			}
			break
		}
	}
	if eventDiscriminator == nil {
		return nil, fmt.Errorf("add_funds method not found in chain config")
	}

	fundsAddedEventLogs, err := ParseSVMFundsAddedEventLog(tx.Meta.LogMessages, eventDiscriminator)
	if err != nil {
		return nil, fmt.Errorf("amount extract failed: %w", err)
	}

	metadata := utxverifiertypes.VerifiedTxMetadata{
		Minted:      false,
		PayloadHash: fundsAddedEventLogs.PayloadHash,
		UsdValue: &utxverifiertypes.USDValue{
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
