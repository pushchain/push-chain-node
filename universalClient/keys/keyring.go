package keys

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	evmhd "github.com/cosmos/evm/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/rs/zerolog/log"
	
	"github.com/pushchain/push-chain-node/universalClient/config"
)

// KeyringConfig holds configuration for keyring initialization
type KeyringConfig struct {
	HomeDir        string
	KeyringBackend KeyringBackend
	HotkeyName     string
	HotkeyPassword string
	OperatorAddr   string
}

// GetKeyringKeybase creates and returns keyring and key info
func GetKeyringKeybase(config KeyringConfig) (keyring.Keyring, string, error) {
	logger := log.Logger.With().Str("module", "GetKeyringKeybase").Logger()
	
	if len(config.HotkeyName) == 0 {
		return nil, "", fmt.Errorf("hotkey name is empty")
	}

	if len(config.HomeDir) == 0 {
		return nil, "", fmt.Errorf("home directory is empty")
	}

	// Prepare password reader for file backend
	var reader io.Reader = strings.NewReader("")
	if config.KeyringBackend == KeyringBackendFile {
		if config.HotkeyPassword == "" {
			return nil, "", fmt.Errorf("password is required for file backend")
		}
		// Keyring expects password twice, each followed by newline
		passwordInput := fmt.Sprintf("%s\n%s\n", config.HotkeyPassword, config.HotkeyPassword)
		reader = strings.NewReader(passwordInput)
	}

	kb, err := CreateKeyring(config.HomeDir, reader, config.KeyringBackend)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get keybase: %w", err)
	}

	// Temporarily disable stdin to avoid prompts
	oldStdIn := os.Stdin
	defer func() {
		os.Stdin = oldStdIn
	}()
	os.Stdin = nil

	logger.Debug().
		Msgf("Checking for Hotkey: %s \nFolder: %s\nBackend: %s", 
			config.HotkeyName, config.HomeDir, kb.Backend())
			
	rc, err := kb.Key(config.HotkeyName)
	if err != nil {
		return nil, "", fmt.Errorf("key not present in backend %s with name (%s): %w", 
			kb.Backend(), config.HotkeyName, err)
	}

	// Get public key in bech32 format
	pubkeyBech32, err := getPubkeyBech32FromRecord(rc)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get pubkey from record: %w", err)
	}

	return kb, pubkeyBech32, nil
}

// CreateNewKey creates a new key in the keyring
func CreateNewKey(kr keyring.Keyring, name string, mnemonic string, passphrase string) (*keyring.Record, error) {
	if mnemonic != "" {
		// Import from mnemonic using EVM algorithm
		return kr.NewAccount(name, mnemonic, passphrase, sdk.FullFundraiserPath, evmhd.EthSecp256k1)
	}
	
	// Generate new key with mnemonic using EVM algorithm
	record, _, err := kr.NewMnemonic(name, keyring.English, sdk.FullFundraiserPath, passphrase, evmhd.EthSecp256k1)
	if err != nil {
		return nil, fmt.Errorf("failed to generate new key with mnemonic: %w", err)
	}
	
	// Return the newly created key record
	return record, nil
}

// CreateNewKeyWithMnemonic creates a new key in the keyring and returns both the record and generated mnemonic
func CreateNewKeyWithMnemonic(kr keyring.Keyring, name string, mnemonic string, passphrase string) (*keyring.Record, string, error) {
	if mnemonic != "" {
		// Import from mnemonic using EVM algorithm
		record, err := kr.NewAccount(name, mnemonic, passphrase, sdk.FullFundraiserPath, evmhd.EthSecp256k1)
		return record, mnemonic, err
	}
	
	// Generate new key with mnemonic using EVM algorithm
	record, generatedMnemonic, err := kr.NewMnemonic(name, keyring.English, sdk.FullFundraiserPath, passphrase, evmhd.EthSecp256k1)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate new key with mnemonic: %w", err)
	}
	
	// Return the newly created key record and generated mnemonic
	return record, generatedMnemonic, nil
}



// CreateInterfaceRegistryWithEVMSupport creates an interface registry with EVM-compatible key types
func CreateInterfaceRegistryWithEVMSupport() codectypes.InterfaceRegistry {
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	
	// Register all key types (both public and private)
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

// CreateKeyring creates a keyring with EVM compatibility
func CreateKeyring(homeDir string, reader io.Reader, keyringBackend KeyringBackend) (keyring.Keyring, error) {
	if len(homeDir) == 0 {
		return nil, fmt.Errorf("home directory is empty")
	}
	
	// Create codec with EVM-compatible key types directly
	registry := CreateInterfaceRegistryWithEVMSupport()
	cdc := codec.NewProtoCodec(registry)

	// Determine backend type
	var backend string
	switch keyringBackend {
	case KeyringBackendFile:
		backend = "file"
	case KeyringBackendTest:
		backend = "test"
	default:
		backend = "test" // Default to test backend
	}

	// Create keyring with appropriate backend and EVM compatibility
	return keyring.New(sdk.KeyringServiceName(), backend, homeDir, reader, cdc, cosmosevmkeyring.Option())
}

// getPubkeyBech32FromRecord extracts bech32 public key from key record
func getPubkeyBech32FromRecord(record *keyring.Record) (string, error) {
	pubkey, err := record.GetPubKey()
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %w", err)
	}

	// For now, return the hex representation of the public key
	// This can be improved later to use proper bech32 encoding
	return fmt.Sprintf("pushpub%x", pubkey.Bytes()), nil
}

// ValidateKeyExists checks if a key exists in the keyring
func ValidateKeyExists(keyring keyring.Keyring, keyName string) error {
	_, err := keyring.Key(keyName)
	if err != nil {
		return fmt.Errorf("key %s not found: %w", keyName, err)
	}
	return nil
}

// CreateKeyringFromConfig creates a keyring with EVM compatibility from config backend type
func CreateKeyringFromConfig(homeDir string, reader io.Reader, configBackend config.KeyringBackend) (keyring.Keyring, error) {
	// Convert config types to keys types
	var keysBackend KeyringBackend
	switch configBackend {
	case config.KeyringBackendFile:
		keysBackend = KeyringBackendFile
	case config.KeyringBackendTest:
		keysBackend = KeyringBackendTest
	default:
		keysBackend = KeyringBackendTest
	}
	
	return CreateKeyring(homeDir, reader, keysBackend)
}