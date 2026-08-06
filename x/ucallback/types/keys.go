package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// UniversalReadsKey is the canonical record for every read request,
	// keyed by requestId. Everything else in this module is an index over it.
	UniversalReadsKey = collections.NewPrefix(1)
)

const (
	ModuleName = "ucallback"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
