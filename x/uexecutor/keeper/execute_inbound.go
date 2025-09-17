package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInbound(ctx context.Context, utx types.UniversalTx) error {
	switch utx.InboundTx.TxType {
	case types.InboundTxType_GAS: // fee abstraction
		return k.ExecuteInboundGasFund(ctx, *utx.InboundTx)

	case types.InboundTxType_FUNDS: // synthetic
		return k.ExecuteInboundBridge(ctx, utx)

	case types.InboundTxType_FUNDS_AND_PAYLOAD: // synthetic + payload
		return k.ExecuteInboundFundsAndPayload(ctx, utx)

	case types.InboundTxType_GAS_AND_PAYLOAD: // fee abstraction + payload
		return k.ExecuteInboundFundsAndPayloadInstant(ctx, utx)

	default:
		return fmt.Errorf("unsupported inbound tx type: %d", utx.InboundTx.TxType)
	}
}
