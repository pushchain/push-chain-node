package main

import (
	"fmt"
	"io"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"

	"github.com/pushchain/push-chain-node/universalClient/config"
)

// getKeybase creates an instance of Keybase for CLI commands
func getKeybase(homeDir string, reader io.Reader, keyringBackend config.KeyringBackend) (keyring.Keyring, error) {
	if len(homeDir) == 0 {
		return nil, fmt.Errorf("home directory is empty")
	}
	
	// Create interface registry and codec with EVM support
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	
	// Explicitly register public key types including EVM-compatible keys
	registry.RegisterImplementations((*cryptotypes.PubKey)(nil),
		&secp256k1.PubKey{},
		&ed25519.PubKey{},
		&evmcrypto.PubKey{},
	)
	// Also register private key implementations for EVM compatibility
	registry.RegisterImplementations((*cryptotypes.PrivKey)(nil),
		&secp256k1.PrivKey{},
		&ed25519.PrivKey{},
		&evmcrypto.PrivKey{},
	)
	
	cdc := codec.NewProtoCodec(registry)

	// Determine backend type
	var backend string
	switch keyringBackend {
	case config.KeyringBackendFile:
		backend = "file"
	case config.KeyringBackendTest:
		backend = "test"
	default:
		backend = "test" // Default to test backend
	}

	// Create keyring with appropriate backend and EVM compatibility
	return keyring.New(sdk.KeyringServiceName(), backend, homeDir, reader, cdc, cosmosevmkeyring.Option())
}