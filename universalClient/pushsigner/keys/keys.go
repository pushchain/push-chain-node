package keysv2

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ UniversalValidatorKeys = &Keys{}

// Keys manages all the keys used by Universal Validator
type Keys struct {
	keyName        string          // Hot key name in keyring
	keyring        keyring.Keyring // Cosmos SDK keyring
	hotkeyPassword string          // Password for file backend
}

// NewKeys creates a new instance of Keys
func NewKeys(
	kr keyring.Keyring,
	keyName string,
	hotkeyPassword string,
) *Keys {
	return &Keys{
		keyName:        keyName,
		keyring:        kr,
		hotkeyPassword: hotkeyPassword,
	}
}

// GetAddress returns the hot key address
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

// GetKeyName returns the name of the hot key in the keyring
func (k *Keys) GetKeyName() string {
	return k.keyName
}

// GetKeyring returns the underlying keyring for signing operations.
// It validates that the key exists in the keyring before returning it.
// For file backend, the keyring handles decryption automatically when signing.
func (k *Keys) GetKeyring() (keyring.Keyring, error) {
	// Validate that the key exists in the keyring
	if _, err := k.keyring.Key(k.keyName); err != nil {
		return nil, fmt.Errorf("key %s not found in keyring: %w", k.keyName, err)
	}
	return k.keyring, nil
}
