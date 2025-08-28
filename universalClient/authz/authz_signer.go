// Package authz provides a signer object for transactions using grants
// grants are used to allow a hotkey to sign transactions on behalf of the operator
package authz

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Signer represents a signer for a grantee key with AuthZ capabilities
type Signer struct {
	KeyType        KeyType        // Type of key being used
	GranterAddress string         // Operator (validator) address
	GranteeAddress sdk.AccAddress // Hot key address
}

// String returns a string representation of a Signer
func (a Signer) String() string {
	return fmt.Sprintf("%s granter:%s grantee:%s", 
		a.KeyType.String(), a.GranterAddress, a.GranteeAddress.String())
}

// signers is a map of all the signers for the different tx types
var signers map[string]Signer

// SetupAuthZSignerList sets the granter and grantee for all the allowed message types
func SetupAuthZSignerList(granter string, grantee sdk.AccAddress) {
	signersList := make(map[string]Signer)
	
	// Create a signer for each allowed message type
	for _, msgType := range AllowedMsgTypes {
		signersList[msgType] = Signer{
			KeyType:        UniversalValidatorHotKey,
			GranterAddress: granter,
			GranteeAddress: grantee,
		}
	}
	
	signers = signersList
}

// GetSigner returns the signer for a given msgURL
func GetSigner(msgURL string) (Signer, error) {
	if signers == nil {
		return Signer{}, fmt.Errorf("signers not initialized, call SetupAuthZSignerList first")
	}
	
	signer, exists := signers[msgURL]
	if !exists {
		return Signer{}, fmt.Errorf("no signer found for message type: %s", msgURL)
	}
	
	return signer, nil
}





// ResetSignersForTesting resets the global signers map (for testing only)
func ResetSignersForTesting() {
	signers = nil
}