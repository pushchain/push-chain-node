package keeper

import (
	"pchain/x/pchain/types"
)

var _ types.QueryServer = Keeper{}
