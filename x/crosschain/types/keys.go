package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// AdminParamsKey saves the current module admin params.
	AdminParamsKey = collections.NewPrefix(1)

	// AdminParamsName is the name of the admin params collection.
	AdminParamsName = "admin_params"
)

const (
	ModuleName = "crosschain"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
