package core

import (
	"context"

	uregistrytypes "github.com/rollchains/pchain/x/uregistry/types"
)

// RegistryInterface defines the interface for registry operations
type RegistryInterface interface {
	GetAllChainConfigs(ctx context.Context) ([]*uregistrytypes.ChainConfig, error)
	GetAllTokenConfigs(ctx context.Context) ([]*uregistrytypes.TokenConfig, error)
}