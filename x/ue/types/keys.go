package types

import (
	fmt "fmt"

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

	InboundSyntheticsKey  = collections.NewPrefix(2)
	InboundSyntheticsName = "inbound_synthetics"
)

const (
	ModuleName = "ue"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetInboundSyntheticKey generates a unique key for an inbound synthetic transaction
func GetInboundSyntheticKey(sourceChain, txHash, logIndex string) string {
	return fmt.Sprintf("%s:%s", sourceChain, txHash, logIndex)
}
