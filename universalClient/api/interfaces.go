package api

import (
	"time"

	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// UniversalClientInterface defines the methods needed by the API server
type UniversalClientInterface interface {
	GetAllChainConfigs() []*uregistrytypes.ChainConfig
	GetAllTokenConfigs() []*uregistrytypes.TokenConfig
	GetTokenConfigsByChain(chain string) []*uregistrytypes.TokenConfig
	GetTokenConfig(chain, address string) *uregistrytypes.TokenConfig
	GetCacheLastUpdate() time.Time
}