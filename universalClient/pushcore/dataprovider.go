package pushcore

import (
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/rs/zerolog"
)

// DataProvider implements coordinator.DataProvider using pushcore.Client to connect to the blockchain.
// This wrapper allows the TSS coordinator to query blockchain state through the existing Client.
type DataProvider struct {
	client *Client
	logger zerolog.Logger
}

// NewDataProvider creates a new data provider using an existing pushcore.Client.
// The caller is responsible for the lifecycle of the underlying Client.
func NewDataProvider(client *Client, logger zerolog.Logger) *DataProvider {
	return &DataProvider{
		client: client,
		logger: logger.With().Str("component", "pushcore_data_provider").Logger(),
	}
}

// GetLatestBlockNum returns the latest block number from the blockchain.
// Implements coordinator.DataProvider interface.
func (p *DataProvider) GetLatestBlockNum() (uint64, error) {
	return p.client.GetLatestBlockNum()
}

// GetUniversalValidators returns all universal validators from the blockchain.
// Implements coordinator.DataProvider interface.
func (p *DataProvider) GetUniversalValidators() ([]*types.UniversalValidator, error) {
	return p.client.GetUniversalValidators()
}

// GetCurrentTSSKeyId returns the current TSS key ID from the blockchain.
// Returns empty string if no key exists yet.
// Implements coordinator.DataProvider interface.
func (p *DataProvider) GetCurrentTSSKeyId() (string, error) {
	return p.client.GetCurrentTSSKeyId()
}

// Ensure DataProvider implements coordinator.DataProvider at compile time
var _ coordinator.DataProvider = (*DataProvider)(nil)
