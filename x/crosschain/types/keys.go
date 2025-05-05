package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)
)

const (
	ModuleName = "crosschain"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
