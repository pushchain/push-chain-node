// Package authz provides a signer object for transactions using grants
// grants are used to allow a hotkey to sign transactions on behalf of the operator
package authz

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Signer represents a signer for a grantee key with AuthZ capabilities
type Signer struct {
	GranterAddress string         // Operator (validator) address
	GranteeAddress sdk.AccAddress // Hot key address
}

// String returns a string representation of a Signer
func (a Signer) String() string {
	return fmt.Sprintf("granter:%s grantee:%s",
		a.GranterAddress, a.GranteeAddress.String())
}

// SignerManager manages AuthZ signers for different message types
type SignerManager struct {
	signers        map[string]Signer
	granterAddress string
	granteeAddress sdk.AccAddress
}

// NewSignerManager creates a new SignerManager with all allowed message types
func NewSignerManager(granter string, grantee sdk.AccAddress) *SignerManager {
	sm := &SignerManager{
		signers:        make(map[string]Signer),
		granterAddress: granter,
		granteeAddress: grantee,
	}

	// Create a signer for each allowed message type
	for _, msgType := range AllowedMsgTypes {
		sm.signers[msgType] = Signer{
			GranterAddress: granter,
			GranteeAddress: grantee,
		}
	}

	return sm
}

// GetSigner returns the signer for a given msgURL
func (sm *SignerManager) GetSigner(msgURL string) (Signer, error) {
	signer, exists := sm.signers[msgURL]
	if !exists {
		return Signer{}, fmt.Errorf("no signer found for message type: %s", msgURL)
	}
	return signer, nil
}

// GetGranterAddress returns the granter address
func (sm *SignerManager) GetGranterAddress() string {
	return sm.granterAddress
}

// GetGranteeAddress returns the grantee address
func (sm *SignerManager) GetGranteeAddress() sdk.AccAddress {
	return sm.granteeAddress
}

