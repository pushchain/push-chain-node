package keeper

import (
	"push/x/push/types"
)

var _ types.QueryServer = Keeper{}
