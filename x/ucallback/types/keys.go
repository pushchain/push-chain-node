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

	// PendingByExpiryKey indexes unsettled reads by the Push Chain height they
	// expire at. Key is (expiryHeight, requestId). Entries are removed the moment
	// a read settles, which makes this the module's set of in-flight work.
	PendingByExpiryKey = collections.NewPrefix(2)
)

const (
	ModuleName = "ucallback"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
