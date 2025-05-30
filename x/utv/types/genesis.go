package types

import (
	"fmt"
)

// DefaultGenesisState returns the default genesis state
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Params:       DefaultParams(),
		ChainConfigs: []ChainConfig{DefaultEthereumSepoliaConfig().ToProto()},
	}
}

// Validate validates the genesis state
func (gs GenesisState) Validate() error {
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Validate chain configs
	chainIDs := make(map[string]bool)
	for _, protoConfig := range gs.ChainConfigs {
		// Convert to internal type for validation
		config := ChainConfigDataFromProto(protoConfig)

		if err := config.Validate(); err != nil {
			return fmt.Errorf("invalid chain config for %s: %w", config.ChainId, err)
		}

		// Check for duplicate chain IDs
		if _, exists := chainIDs[config.ChainId]; exists {
			return fmt.Errorf("duplicate chain ID %s", config.ChainId)
		}
		chainIDs[config.ChainId] = true
	}

	return nil
}
