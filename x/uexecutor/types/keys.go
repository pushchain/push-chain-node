package types

import (
	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// ChainConfigsKey saves the current module chainConfigs collection prefix
	ChainConfigsKey = collections.NewPrefix(1)

	// ChainConfigsName is the name of the chainConfigs collection.
	ChainConfigsName = "chain_configs"
)

const (
	ModuleName = "uexecutor"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)
