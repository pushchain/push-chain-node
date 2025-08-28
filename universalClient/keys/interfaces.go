package keys

import (
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// KeyringBackend represents the type of keyring backend to use
type KeyringBackend string

const (
	// KeyringBackendTest is the test Cosmos keyring backend (unencrypted)
	KeyringBackendTest KeyringBackend = "test"
	
	// KeyringBackendFile is the file Cosmos keyring backend (encrypted)
	KeyringBackendFile KeyringBackend = "file"
)

// String returns the string representation of the keyring backend
func (kb KeyringBackend) String() string {
	return string(kb)
}

// UniversalValidatorKeys defines the interface for key management in Universal Validator
type UniversalValidatorKeys interface {
	// GetAddress returns the hot key address
	GetAddress() (sdk.AccAddress, error)
	
	// GetPrivateKey returns the hot key private key (requires password)
	GetPrivateKey(password string) (cryptotypes.PrivKey, error)
	
	// GetHotkeyPassword returns the password for file backend
	GetHotkeyPassword() string
}