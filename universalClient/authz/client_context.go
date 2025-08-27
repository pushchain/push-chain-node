package authz

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/std"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authztypes "github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rs/zerolog"
)

// ClientContextConfig holds configuration for creating a client context
type ClientContextConfig struct {
	ChainID        string
	NodeURI        string
	GRPCEndpoint   string
	Keys           keys.UniversalValidatorKeys
	Logger         zerolog.Logger
}

// CreateClientContext creates a properly configured client context for transaction signing
func CreateClientContext(config ClientContextConfig) (client.Context, error) {
	// Create codec registry
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	
	// Register standard interfaces
	std.RegisterInterfaces(interfaceRegistry)
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	
	// Register auth module interfaces
	authtypes.RegisterInterfaces(interfaceRegistry)
	authztypes.RegisterInterfaces(interfaceRegistry)
	
	// Create codec
	cdc := codec.NewProtoCodec(interfaceRegistry)

	// Create tx config
	txConfig := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)

	// Get keyring from keys
	keyring := config.Keys.GetKeybase()

	// Create client context
	clientCtx := client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(interfaceRegistry).
		WithTxConfig(txConfig).
		WithLegacyAmino(codec.NewLegacyAmino()).
		WithInput(nil). // No input for automated signing
		WithAccountRetriever(authtypes.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastSync).
		WithKeyring(keyring).
		WithChainID(config.ChainID).
		WithSimulation(false).
		WithOffline(false).
		WithSkipConfirmation(true)

	// Set node URI if provided
	if config.NodeURI != "" {
		clientCtx = clientCtx.WithNodeURI(config.NodeURI)
	}

	// Set gRPC endpoint if provided  
	if config.GRPCEndpoint != "" {
		clientCtx = clientCtx.WithGRPCClient(nil) // Will be created lazily
	}

	return clientCtx, nil
}

// ValidateClientContext validates that a client context has required fields
func ValidateClientContext(clientCtx client.Context) error {
	if clientCtx.ChainID == "" {
		return fmt.Errorf("chain ID is required")
	}

	if clientCtx.TxConfig == nil {
		return fmt.Errorf("tx config is required")
	}

	if clientCtx.Codec == nil {
		return fmt.Errorf("codec is required")
	}

	if clientCtx.Keyring == nil {
		return fmt.Errorf("keyring is required")
	}

	return nil
}

// GetDefaultGasConfig returns default gas configuration for Universal Validator transactions
func GetDefaultGasConfig() GasConfig {
	return GasConfig{
		GasAdjustment: 1.2,    // 20% buffer
		GasPrices:     "0push", // Gas-free transactions (operator pays)
		MaxGas:        1000000, // 1M gas limit
	}
}

// GasConfig holds gas-related configuration
type GasConfig struct {
	GasAdjustment float64
	GasPrices     string
	MaxGas        uint64
}

// UpdateClientContextForTesting updates client context for testing scenarios
func UpdateClientContextForTesting(clientCtx client.Context) client.Context {
	return clientCtx.
		WithSimulation(true).
		WithOffline(true).
		WithBroadcastMode(flags.BroadcastSync)
}