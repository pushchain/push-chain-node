package types

import (
	fmt "fmt"
	"strings"

	"cosmossdk.io/collections"

	"github.com/pushchain/push-chain-node/utils"
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

	// TokenConfigsKey saves the current module tokenConfigs collection prefix
	TokenConfigsKey = collections.NewPrefix(2)

	// TokenConfigsName is the name of the tokenConfigs collection.
	TokenConfigsName = "token_configs"

	// PRC20Index secondary index on TokenConfigs: canonical (EIP-55) PRC20 → storage key.
	PRC20IndexKey  = collections.NewPrefix(3)
	PRC20IndexName = "prc20_index"
)

const (
	ModuleName = "uregistry"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetTokenConfigsStorageKey builds the "chain:address" key with the address
// canonicalized per namespace, so case variants map to one row. Strict format
// enforcement is in TokenConfig.ValidateBasic.
func GetTokenConfigsStorageKey(chain, address string) string {
	return fmt.Sprintf("%s:%s", chain, CanonicalTokenAddress(chain, address))
}

// CanonicalTokenAddress is the lenient canonicalizer used for key paths:
// canonical form when the address parses, trimmed input otherwise.
func CanonicalTokenAddress(chain, address string) string {
	canon, err := utils.CanonicalizeAddressByNamespace(chain, address)
	if err != nil {
		return strings.TrimSpace(address)
	}
	return canon
}
