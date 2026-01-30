package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	core "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// EVMHooks implements the EVM post-processing hooks.
// This hook will be invoked after every EVM transaction execution
// and is responsible for detecting outbound events and creating UniversalTx if needed.
type EVMHooks struct {
	k Keeper
}

// NewEVMHooks creates a new instance of EVMHooks.
func NewEVMHooks(k Keeper) evmtypes.EvmHooks {
	return EVMHooks{k: k}
}

// PostTxProcessing is called by the EVM module after transaction execution.
// It inspects the receipt and creates UniversalTx + Outbound only if
// UniversalTxWithdraw event is detected.
func (h EVMHooks) PostTxProcessing(
	ctx sdk.Context,
	sender common.Address,
	msg core.Message,
	receipt *ethtypes.Receipt,
) error {
	if receipt == nil || len(receipt.Logs) == 0 {
		return nil
	}

	protoReceipt := &evmtypes.MsgEthereumTxResponse{
		Hash:    receipt.TxHash.Hex(),
		GasUsed: receipt.GasUsed,
		Logs:    convertReceiptLogs(receipt.Logs),
	}

	// Build pcTx representation
	pcTx := types.PCTx{
		Sender:      sender.Hex(),
		TxHash:      protoReceipt.Hash,
		GasUsed:     protoReceipt.GasUsed,
		BlockHeight: uint64(ctx.BlockHeight()),
		Status:      "SUCCESS",
	}

	// This will:
	// - check if outbound exists
	// - create universal tx if needed
	// - attach outbounds
	// - emit events
	return h.k.CreateUniversalTxFromReceiptIfOutbound(ctx, protoReceipt, pcTx)
}

func convertReceiptLogs(logs []*ethtypes.Log) []*evmtypes.Log {
	out := make([]*evmtypes.Log, 0, len(logs))

	for _, l := range logs {
		out = append(out, &evmtypes.Log{
			Address:     l.Address.Hex(),
			Topics:      convertTopics(l.Topics),
			Data:        l.Data,
			BlockNumber: l.BlockNumber,
			TxHash:      l.TxHash.Hex(),
			TxIndex:     uint64(l.TxIndex),
			BlockHash:   l.BlockHash.Hex(),
			Index:       uint64(l.Index),
			Removed:     l.Removed,
		})
	}

	return out
}

func convertTopics(topics []common.Hash) []string {
	out := make([]string, len(topics))
	for i, t := range topics {
		out[i] = t.Hex()
	}
	return out
}
