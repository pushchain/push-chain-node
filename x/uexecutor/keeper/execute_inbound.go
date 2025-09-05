package keeper

import (
	"context"
	"fmt"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) ExecuteInbound(ctx context.Context, utx types.UniversalTx) error {
	switch utx.InboundTx.TxType {
	case types.InboundTxType_GAS_FUND_TX: // fee abstraction
		return k.ExecuteInboundGasFund(ctx, *utx.InboundTx)

	case types.InboundTxType_FUNDS_BRIDGE_TX: // synthetic
		return k.ExecuteInboundBridge(ctx, utx)

	case types.InboundTxType_FUNDS_AND_PAYLOAD_TX: // synthetic + payload
		return k.ExecuteInboundFundsAndPayload(ctx, utx)

	case types.InboundTxType_FUNDS_AND_PAYLOAD_INSTANT_TX: // fee abstraction + payload
		return k.ExecuteInboundFundsAndPayloadInstant(ctx, utx)

	default:
		return fmt.Errorf("unsupported inbound tx type: %d", utx.InboundTx.TxType)
	}
}
