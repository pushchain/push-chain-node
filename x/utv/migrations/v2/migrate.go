package v2

import (
	"fmt"
	"strings"

	"cosmossdk.io/collections"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/pushchain/push-chain-node/utils"
	"github.com/pushchain/push-chain-node/utils/rpc"
	evmrpc "github.com/pushchain/push-chain-node/utils/rpc/evm"
	uetypes "github.com/pushchain/push-chain-node/x/ue/types"
	"github.com/pushchain/push-chain-node/x/utv/keeper"
	utvtypes "github.com/pushchain/push-chain-node/x/utv/types"
)

// Migration from Map[string, bool] to Map[string, VerifiedTxMetadata]
func MigrateVerifiedTxsToMetadata(ctx sdk.Context, k *keeper.Keeper, cdc codec.BinaryCodec) error {
	sb := k.SchemaBuilder()

	oldMap := collections.NewMap(
		sb,
		utvtypes.VerifiedTxsKeyPrefix,
		utvtypes.VerifiedTxsName,
		collections.StringKey,
		collections.BoolValue,
	)

	return oldMap.Walk(ctx, nil, func(storageKey string, verified bool) (stop bool, err error) {
		if !verified {
			return false, nil
		}

		// Example: "eip155:11155111:0xabc123..." or "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1:4tTvV..."
		parts := strings.Split(storageKey, ":")
		if len(parts) < 3 {
			ctx.Logger().Error("Invalid storage key format", "key", storageKey)
			return false, nil
		}

		chain := strings.Join(parts[:2], ":")
		txHash := strings.Join(parts[2:], ":")

		if chain == "solana:etwtrabzayq6imfeykouru166vu2xqa1" {
			chain = "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
		}

		chainConfig, err := k.GetUEKeeper().GetChainConfig(ctx, chain)
		if err != nil {
			ctx.Logger().Error("Failed to get chain config", "chain", chain, "err", err)
			return false, nil
		}

		var meta *utvtypes.VerifiedTxMetadata

		switch chainConfig.VmType {
		case uetypes.VM_TYPE_EVM:
			rpcCfg := rpc.RpcCallConfig{
				PrivateRPC: utils.GetEnvRPCOverride(chainConfig.Chain),
				PublicRPC:  chainConfig.PublicRpcUrl,
			}

			receipt, err := evmrpc.EVMGetTransactionReceipt(ctx, rpcCfg, txHash)
			if err != nil {
				return false, fmt.Errorf("fetch receipt failed: %w", err)
			}
			from := receipt.From
			meta, err = k.VerifyEVMInboundTx(ctx, from, txHash, chainConfig)

		case uetypes.VM_TYPE_SVM:
			// ⚠️ Skipping SVM txHash migration due to known corrupted base58-encoded lowercase entries
			ctx.Logger().Info("⏭️ Skipping corrupted SVM txHash", "txHash", txHash)
			return false, nil

		default:
			ctx.Logger().Error("Unknown VM type", "vmType", chainConfig.VmType)
			return false, nil
		}

		if err != nil {
			ctx.Logger().Error("Verification failed", "txHash", txHash, "chain", chain, "err", err)
			return false, nil
		}

		meta.Minted = true

		if err := k.StoreVerifiedInboundTx(ctx, chain, txHash, *meta); err != nil {
			ctx.Logger().Error("Failed to store verified inbound tx", "txHash", txHash, "err", err)
			return false, err
		}

		// ✅ Delete old key
		if err := oldMap.Remove(ctx, storageKey); err != nil {
			ctx.Logger().Error("Failed to delete old VerifiedTx entry", "key", storageKey, "err", err)
			return false, err
		}

		ctx.Logger().Info(fmt.Sprintf("✅ Migrated and removed tx %s from old storage", txHash))
		return false, nil
	})
}
