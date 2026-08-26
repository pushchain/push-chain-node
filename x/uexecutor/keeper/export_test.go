package keeper

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func (k Keeper) TestAttachOutboundsToUtx(ctx sdk.Context, utxId string, outbounds []*types.OutboundTx, revertMsg string) error {
	return k.attachOutboundsToUtx(ctx, utxId, outbounds, revertMsg)
}

func (k Keeper) TestBuildRevertOutbound(ctx sdk.Context, inbound *types.Inbound) (*types.OutboundTx, error) {
	return k.buildRevertOutbound(ctx, inbound)
}
