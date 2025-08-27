package keys

import (
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rs/zerolog/log"
)

var (
	// ErrBech32ifyPubKey is an error when Bech32ifyPubKey fails
	ErrBech32ifyPubKey = errors.New("Bech32ifyPubKey fail in main")

	// ErrNewPubKey is an error when NewPubKey fails
	ErrNewPubKey = errors.New("NewPubKey error from string")
)

var _ UniversalValidatorKeys = &Keys{}

// Keys manages all the keys used by Universal Validator
type Keys struct {
	signerName       string                // Hot key name in keyring
	kb               keyring.Keyring       // Cosmos SDK keyring
	OperatorAddress  sdk.AccAddress        // Operator (validator) address for reference
	hotkeyPassword   string               // Password for file backend
}

// NewKeys creates a new instance of Keys from configuration
func NewKeys(hotkeyName string, cfg *config.Config) (*Keys, error) {
	if hotkeyName == "" {
		return nil, fmt.Errorf("hotkey name is required")
	}

	// Parse operator address
	operatorAddr, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
	if err != nil {
		return nil, fmt.Errorf("invalid operator address %s: %w", cfg.AuthzGranter, err)
	}

	// Convert config KeyringBackend to keys KeyringBackend
	var keyringBackend KeyringBackend
	switch cfg.KeyringBackend {
	case config.KeyringBackendFile:
		keyringBackend = KeyringBackendFile
	case config.KeyringBackendTest:
		keyringBackend = KeyringBackendTest
	default:
		keyringBackend = KeyringBackendTest // Default to test backend
	}

	// Create keyring config
	keyringConfig := KeyringConfig{
		HomeDir:        config.GetKeyringDir(cfg),
		KeyringBackend: keyringBackend,
		HotkeyName:     hotkeyName,
		HotkeyPassword: "", // No password for simplified version
		OperatorAddr:   cfg.AuthzGranter,
	}

	// Initialize keyring
	kb, _, err := GetKeyringKeybase(keyringConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize keyring: %w", err)
	}

	return &Keys{
		signerName:      hotkeyName,
		kb:              kb,
		OperatorAddress: operatorAddr,
		hotkeyPassword:  "",
	}, nil
}

// NewKeysWithKeybase creates a new instance of Keys
func NewKeysWithKeybase(
	kb keyring.Keyring,
	operatorAddress sdk.AccAddress,
	hotkeyName string,
	hotkeyPassword string,
) *Keys {
	return &Keys{
		signerName:      hotkeyName,
		kb:              kb,
		OperatorAddress: operatorAddress,
		hotkeyPassword:  hotkeyPassword,
	}
}

// GetHotkeyKeyName returns the hot key name
func GetHotkeyKeyName(signerName string) string {
	return signerName
}

// GetSignerInfo returns the key record for the hot key
func (k *Keys) GetSignerInfo() *keyring.Record {
	signer := GetHotkeyKeyName(k.signerName)
	info, err := k.kb.Key(signer)
	if err != nil {
		log.Error().Err(err).Msgf("Failed to get key info for %s", signer)
		return nil
	}
	return info
}

// GetOperatorAddress returns the operator address
func (k *Keys) GetOperatorAddress() sdk.AccAddress {
	return k.OperatorAddress
}

// GetAddress returns the hot key address
func (k *Keys) GetAddress() (sdk.AccAddress, error) {
	signer := GetHotkeyKeyName(k.signerName)
	info, err := k.kb.Key(signer)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", signer, err)
	}
	
	addr, err := info.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from key info: %w", err)
	}
	
	return addr, nil
}

// GetPrivateKey returns the private key (requires password for file backend)
func (k *Keys) GetPrivateKey(password string) (cryptotypes.PrivKey, error) {
	signer := GetHotkeyKeyName(k.signerName)
	
	// For file backend, use provided password; for test backend, password is ignored
	var actualPassword string
	if k.kb.Backend() == keyring.BackendFile {
		if password == "" {
			return nil, fmt.Errorf("password is required for file backend")
		}
		actualPassword = password
	}
	
	privKeyArmor, err := k.kb.ExportPrivKeyArmor(signer, actualPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to export private key: %w", err)
	}
	
	priKey, _, err := crypto.UnarmorDecryptPrivKey(privKeyArmor, actualPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to unarmor private key: %w", err)
	}
	
	return priKey, nil
}

// GetKeybase returns the keybase
func (k *Keys) GetKeybase() keyring.Keyring {
	return k.kb
}

// GetHotkeyPassword returns the password to be used
// returns empty if no password is needed (test backend)
func (k *Keys) GetHotkeyPassword() string {
	if k.GetKeybase().Backend() == keyring.BackendFile {
		return k.hotkeyPassword
	}
	return ""
}

