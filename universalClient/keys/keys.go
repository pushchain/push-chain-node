package keys

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)


var _ UniversalValidatorKeys = &Keys{}

// Keys manages all the keys used by Universal Validator
type Keys struct {
	signerName       string                // Hot key name in keyring
	kb               keyring.Keyring       // Cosmos SDK keyring
	OperatorAddress  sdk.AccAddress        // Operator (validator) address for reference
	hotkeyPassword   string               // Password for file backend
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



// GetAddress returns the hot key address
func (k *Keys) GetAddress() (sdk.AccAddress, error) {
	info, err := k.kb.Key(k.signerName)
	if err != nil {
		return nil, fmt.Errorf("failed to get key %s: %w", k.signerName, err)
	}
	
	addr, err := info.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get address from key info: %w", err)
	}
	
	return addr, nil
}

// GetPrivateKey returns the private key (requires password for file backend)
func (k *Keys) GetPrivateKey(password string) (cryptotypes.PrivKey, error) {
	// For file backend, use provided password; for test backend, password is ignored
	var actualPassword string
	if k.kb.Backend() == keyring.BackendFile {
		if password == "" {
			return nil, fmt.Errorf("password is required for file backend")
		}
		actualPassword = password
	}
	
	privKeyArmor, err := k.kb.ExportPrivKeyArmor(k.signerName, actualPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to export private key: %w", err)
	}
	
	priKey, _, err := crypto.UnarmorDecryptPrivKey(privKeyArmor, actualPassword)
	if err != nil {
		return nil, fmt.Errorf("failed to unarmor private key: %w", err)
	}
	
	return priKey, nil
}


// GetHotkeyPassword returns the password to be used
// returns empty if no password is needed (test backend)
func (k *Keys) GetHotkeyPassword() string {
	if k.kb.Backend() == keyring.BackendFile {
		return k.hotkeyPassword
	}
	return ""
}

