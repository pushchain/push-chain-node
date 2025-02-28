package keeper

import (
	"pushchain/x/pushchain/types"
)

var _ types.QueryServer = Keeper{}
