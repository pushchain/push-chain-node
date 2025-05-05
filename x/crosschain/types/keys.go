package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// AdminParamsKey saves the current module admin params.
	AdminParamsKey = collections.NewPrefix(1)
)

const (
	ModuleName = "crosschain"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
