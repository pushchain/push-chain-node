package evm

import (
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
)

// TransactionBuilder handles building EVM transactions for the gateway
type TransactionBuilder struct {
	parentClient *Client
	gatewayAddr  ethcommon.Address
	logger       zerolog.Logger
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(
	parentClient *Client,
	gatewayAddr ethcommon.Address,
	logger zerolog.Logger,
) *TransactionBuilder {
	return &TransactionBuilder{
		parentClient: parentClient,
		gatewayAddr:  gatewayAddr,
		logger:       logger.With().Str("component", "evm_tx_builder").Logger(),
	}
}

// Note: Transaction building methods will be added here when needed
// Currently EVM client is read-only and doesn't build transactions