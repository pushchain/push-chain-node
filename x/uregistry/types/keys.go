package types

import (
	fmt "fmt"
	"strings"

	"cosmossdk.io/collections"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// SystemConfigKey saves the current module system config
	SystemConfigKey = collections.NewPrefix(1)

	// SystemConfigName is the name of the systemConfig collection.
	SystemConfigName = "system_config"

	// ChainConfigsKey saves the current module chainConfigs collection prefix
	ChainConfigsKey = collections.NewPrefix(2)

	// ChainConfigsName is the name of the chainConfigs collection.
	ChainConfigsName = "chain_configs"

	// TokenConfigsKey saves the current module tokenConfigs collection prefix
	TokenConfigsKey = collections.NewPrefix(3)

	// TokenConfigsName is the name of the tokenConfigs collection.
	TokenConfigsName = "token_configs"
)

const (
	ModuleName = "uregistry"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetTokenConfigsStorageKey returns the storage key for token config storage in the format "chain:address".
func GetTokenConfigsStorageKey(chain, address string) string {
	// Normalize to lowercase and strip whitespace
	return fmt.Sprintf("%s:%s", chain, strings.TrimSpace(address))
}
