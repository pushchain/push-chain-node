package keysv2

import (
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
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

// UniversalValidatorKeys defines the interface for key management in Universal Validator
type UniversalValidatorKeys interface {
	// GetAddress returns the hot key address
	GetAddress() (sdk.AccAddress, error)

	// GetKeyName returns the name of the hot key in the keyring
	GetKeyName() string

	// GetKeyring returns the underlying keyring for signing operations.
	// It validates that the key exists before returning the keyring.
	// For file backend, decryption happens automatically when signing via tx.Sign().
	// This allows signing without exposing the private key.
	GetKeyring() (keyring.Keyring, error)
}
