// Package keys provides keyring management for the Push Universal Validator.
package keys

import (
	"fmt"
	"io"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"
	evmhd "github.com/cosmos/evm/crypto/hd"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"

	"github.com/pushchain/push-chain-node/universalClient/config"
)

// Keys wraps a Cosmos SDK keyring and a specific key name within it.
type Keys struct {
	keyName string
	keyring keyring.Keyring
}

// NewKeys creates a new Keys instance.
func NewKeys(kr keyring.Keyring, keyName string) *Keys {
	return &Keys{
		keyName: keyName,
		keyring: kr,
	}
}

// GetAddress returns the address of the key.
func (k *Keys) GetAddress() (sdk.AccAddress, error) {
	info, err := k.keyring.Key(k.keyName)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", k.keyName, err)
	}

	addr, err := info.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from key info: %w", err)
	}

	return addr, nil
}

// GetKeyName returns the name of the key in the keyring.
func (k *Keys) GetKeyName() string {
	return k.keyName
}

// GetKeyring validates the key exists and returns the underlying keyring for signing.
func (k *Keys) GetKeyring() (keyring.Keyring, error) {
	if _, err := k.keyring.Key(k.keyName); err != nil {
		return nil, fmt.Errorf("key %s not found in keyring: %w", k.keyName, err)
	}
	return k.keyring, nil
}

// CreateKeyring creates an EVM-compatible keyring.
func CreateKeyring(homeDir string, reader io.Reader, backend config.KeyringBackend) (keyring.Keyring, error) {
	if homeDir == "" {
		return nil, fmt.Errorf("home directory is empty")
	}

	registry := NewInterfaceRegistryWithEVMSupport()
	cdc := codec.NewProtoCodec(registry)

	backendStr := "test"
	if backend == config.KeyringBackendFile {
		backendStr = "file"
	}

	return keyring.New(sdk.KeyringServiceName(), backendStr, homeDir, reader, cdc, cosmosevmkeyring.Option())
}

// CreateNewKey creates a new key in the keyring and returns the record and mnemonic.
// If mnemonic is provided, it imports the key; otherwise generates a new one.
func CreateNewKey(kr keyring.Keyring, name string, mnemonic string, passphrase string) (*keyring.Record, string, error) {
	if mnemonic != "" {
		record, err := kr.NewAccount(name, mnemonic, passphrase, sdk.FullFundraiserPath, evmhd.EthSecp256k1)
		return record, mnemonic, err
	}

	record, generatedMnemonic, err := kr.NewMnemonic(name, keyring.English, sdk.FullFundraiserPath, passphrase, evmhd.EthSecp256k1)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate new key with mnemonic: %w", err)
	}

	return record, generatedMnemonic, nil
}

// NewInterfaceRegistryWithEVMSupport creates an interface registry with EVM-compatible key types.
func NewInterfaceRegistryWithEVMSupport() codectypes.InterfaceRegistry {
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)

	registry.RegisterImplementations((*cryptotypes.PubKey)(nil),
		&secp256k1.PubKey{},
		&ed25519.PubKey{},
		&evmcrypto.PubKey{},
	)
	registry.RegisterImplementations((*cryptotypes.PrivKey)(nil),
		&secp256k1.PrivKey{},
		&ed25519.PrivKey{},
		&evmcrypto.PrivKey{},
	)

	return registry
}
