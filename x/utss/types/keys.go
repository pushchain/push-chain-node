package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"
)

const (
	ModuleName = "utss"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
