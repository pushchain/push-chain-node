package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/common"
	core "github.com/ethereum/go-ethereum/core"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

// EVMHooks implements the EVM post-processing hooks for x/ucallback.
//
// Runs after every EVM transaction, so the log filter in IngestReadRequests must
// stay tight: this hook sees traffic for the whole chain and must be a no-op for
// all of it except UniversalCallback's ReadRequested events.
type EVMHooks struct {
	k Keeper
}

// NewEVMHooks creates a new instance of EVMHooks.
func NewEVMHooks(k Keeper) evmtypes.EvmHooks {
	return EVMHooks{k: k}
}

// PostTxProcessing inspects the receipt and records a UniversalRead for every
// ReadRequested event the transaction emitted.
//
// Returning an error reverts the whole EVM transaction. That is the behaviour we
// want here: a ReadRequested log we cannot record is a request the user paid for
// that no validator would ever serve. Reverting returns their fee instead of
// stranding it.
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

	return h.k.IngestReadRequests(ctx, protoReceipt)
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
