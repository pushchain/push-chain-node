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

// SignerManager manages AuthZ signers for message validation
type SignerManager struct {
	granterAddress    string
	granteeAddress    sdk.AccAddress
	allowedMsgTypes   []string
}

// NewSignerManager creates a new SignerManager with default allowed message types
func NewSignerManager(granter string, grantee sdk.AccAddress) *SignerManager {
	return &SignerManager{
		granterAddress:  granter,
		granteeAddress:  grantee,
		allowedMsgTypes: DefaultAllowedMsgTypes,
	}
}

// NewSignerManagerWithMsgTypes creates a new SignerManager with custom allowed message types
func NewSignerManagerWithMsgTypes(granter string, grantee sdk.AccAddress, allowedMsgTypes []string) *SignerManager {
	return &SignerManager{
		granterAddress:  granter,
		granteeAddress:  grantee,
		allowedMsgTypes: allowedMsgTypes,
	}
}

// GetSigner returns the signer for a given msgURL if it's allowed
func (sm *SignerManager) GetSigner(msgURL string) (Signer, error) {
	if !sm.isAllowedMsgType(msgURL) {
		return Signer{}, fmt.Errorf("no signer found for message type: %s", msgURL)
	}
	return Signer{
		GranterAddress: sm.granterAddress,
		GranteeAddress: sm.granteeAddress,
	}, nil
}

// GetGranterAddress returns the granter address
func (sm *SignerManager) GetGranterAddress() string {
	return sm.granterAddress
}

// GetGranteeAddress returns the grantee address
func (sm *SignerManager) GetGranteeAddress() sdk.AccAddress {
	return sm.granteeAddress
}

// isAllowedMsgType checks if a message type is allowed for this SignerManager
func (sm *SignerManager) isAllowedMsgType(msgType string) bool {
	for _, allowedType := range sm.allowedMsgTypes {
		if allowedType == msgType {
			return true
		}
	}
	return false
}

