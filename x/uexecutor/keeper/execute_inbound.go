package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInbound(ctx context.Context, utx types.UniversalTx) error {
	k.Logger().Info("execute inbound dispatched",
		"utx_key", utx.Id,
		"tx_type", utx.InboundTx.TxType.String(),
		"source_chain", utx.InboundTx.SourceChain,
		"amount", utx.InboundTx.Amount,
	)

	switch utx.InboundTx.TxType {
	case types.TxType_GAS: // fee abstraction
		return k.ExecuteInboundGas(ctx, *utx.InboundTx)

	case types.TxType_FUNDS: // synthetic
		return k.ExecuteInboundFunds(ctx, utx)

	case types.TxType_FUNDS_AND_PAYLOAD: // synthetic + payload
		return k.ExecuteInboundFundsAndPayload(ctx, utx)

	case types.TxType_GAS_AND_PAYLOAD: // fee abstraction + payload
		return k.ExecuteInboundGasAndPayload(ctx, utx)

	default:
		k.Logger().Error("unsupported inbound tx type", "utx_key", utx.Id, "tx_type", utx.InboundTx.TxType)
		return fmt.Errorf("unsupported inbound tx type: %d", utx.InboundTx.TxType)
	}
}
