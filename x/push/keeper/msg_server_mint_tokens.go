package keeper

import (
	"context"

	"push/x/push/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

func (k msgServer) MintTokens(goCtx context.Context, msg *types.MsgMintTokens) (*types.MsgMintTokensResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Mint coins
	err := k.bankKeeper.MintCoins(ctx, types.ModuleName, msg.Amount)
	if err != nil {
		return nil, err
	}

	// Convert creator address string to AccAddress
	creatorAddr, err := sdk.AccAddressFromBech32(msg.Creator)
	if err != nil {
		return nil, err
	}

	// Send to creator
	err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, creatorAddr, msg.Amount)
	if err != nil {
		return nil, err
	}

	return &types.MsgMintTokensResponse{}, nil
}
