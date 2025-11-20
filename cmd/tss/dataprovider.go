package main

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

// StaticPushChainDataProvider implements PushChainDataProvider for demo/testing.
// It reads validator information from the shared node registry file.
type StaticPushChainDataProvider struct {
	validatorAddress string
	logger           zerolog.Logger
}

// NewStaticPushChainDataProvider creates a new static data provider.
func NewStaticPushChainDataProvider(validatorAddress string, logger zerolog.Logger) *StaticPushChainDataProvider {
	return &StaticPushChainDataProvider{
		validatorAddress: validatorAddress,
		logger:           logger,
	}
}

// GetLatestBlockNum returns the latest block number.
func (p *StaticPushChainDataProvider) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	// Use timestamp as block number for demo
	return uint64(time.Now().Unix()), nil
}

// GetUniversalValidators returns all universal validators.
func (p *StaticPushChainDataProvider) GetUniversalValidators(ctx context.Context) ([]*tss.UniversalValidator, error) {
	// Read nodes from shared registry file
	nodes, err := readNodeRegistry(p.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to read node registry: %w", err)
	}

	// Convert to UniversalValidator list
	validators := make([]*tss.UniversalValidator, 0, len(nodes))
	for _, node := range nodes {
		validators = append(validators, &tss.UniversalValidator{
			ValidatorAddress: node.ValidatorAddress,
			Status:           tss.UVStatusActive,
			Network: tss.NetworkInfo{
				PeerID:     node.PeerID,
				Multiaddrs: node.Multiaddrs,
			},
			JoinedAtBlock: 0,
		})
	}

	return validators, nil
}

// GetUniversalValidator returns a specific universal validator by address.
func (p *StaticPushChainDataProvider) GetUniversalValidator(ctx context.Context, validatorAddress string) (*tss.UniversalValidator, error) {
	validators, err := p.GetUniversalValidators(ctx)
	if err != nil {
		return nil, err
	}
	for _, v := range validators {
		if v.ValidatorAddress == validatorAddress {
			return v, nil
		}
	}
	return nil, fmt.Errorf("validator not found: %s", validatorAddress)
}
