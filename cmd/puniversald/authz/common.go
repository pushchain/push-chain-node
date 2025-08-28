package authz

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/rollchains/pchain/universalClient/keys"
)


// setupClientContext creates a client context with all required interfaces registered
func setupClientContext(kb keyring.Keyring, chainID, rpcEndpoint string) (client.Context, error) {
	// Create gRPC connection
	conn, err := grpc.NewClient(rpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return client.Context{}, fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}

	// Setup codec with all required interfaces using shared EVM registry
	registry := keys.CreateInterfaceRegistryWithEVMSupport()
	authz.RegisterInterfaces(registry)
	authtypes.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	stakingtypes.RegisterInterfaces(registry)
	govtypes.RegisterInterfaces(registry)
	
	cdc := codec.NewProtoCodec(registry)

	// Create TxConfig
	txConfig := tx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})

	// Create client context
	return client.Context{}.
		WithCodec(cdc).
		WithInterfaceRegistry(registry).
		WithChainID(chainID).
		WithKeyring(kb).
		WithGRPCClient(conn).
		WithTxConfig(txConfig), nil
}



// resolveAccountAddress resolves account string to address (can be address or key name)
func resolveAccountAddress(account string, kb keyring.Keyring) (sdk.AccAddress, error) {
	// Try to parse as address first, then as key name
	if addr, err := sdk.AccAddressFromBech32(account); err == nil {
		return addr, nil
	}

	// Try as key name
	record, err := kb.Key(account)
	if err != nil {
		return nil, fmt.Errorf("account '%s' not found as address or key name: %w", account, err)
	}

	return record.GetAddress()
}

// ensureGRPCPort appends the standard gRPC port to the base URL
func ensureGRPCPort(endpoint string) string {
	// Config contains clean base URLs, append standard gRPC port
	return endpoint + ":9090"
}