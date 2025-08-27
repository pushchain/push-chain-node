// Package authz provides a signer object for transactions using grants
// grants are used to allow a hotkey to sign transactions on behalf of the operator
package authz

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/x/authz"
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

// WrapWithAuthZ wraps a message with MsgExec for AuthZ execution
func WrapWithAuthZ(msg sdk.Msg, signer Signer) (*authz.MsgExec, error) {
	// Validate that the message type is allowed
	msgTypeURL := sdk.MsgTypeURL(msg)
	if !IsAllowedMsgType(msgTypeURL) {
		return nil, fmt.Errorf("message type %s is not allowed for AuthZ execution", msgTypeURL)
	}
	
	// Create MsgExec with the grantee address and the message
	authzMessage := authz.NewMsgExec(signer.GranteeAddress, []sdk.Msg{msg})
	
	return &authzMessage, nil
}

// ValidateSigner checks if the signer is properly configured
func ValidateSigner(signer Signer) error {
	if signer.GranterAddress == "" {
		return fmt.Errorf("granter address cannot be empty")
	}
	
	if signer.GranteeAddress.Empty() {
		return fmt.Errorf("grantee address cannot be empty")
	}
	
	// Validate granter address format
	_, err := sdk.AccAddressFromBech32(signer.GranterAddress)
	if err != nil {
		return fmt.Errorf("invalid granter address format: %w", err)
	}
	
	return nil
}

// GetAllSigners returns all configured signers
func GetAllSigners() map[string]Signer {
	if signers == nil {
		return make(map[string]Signer)
	}
	
	// Return a copy to prevent external modification
	result := make(map[string]Signer)
	for k, v := range signers {
		result[k] = v
	}
	
	return result
}

// IsSignerConfigured checks if signers have been configured
func IsSignerConfigured() bool {
	return len(signers) > 0
}

// ResetSignersForTesting resets the global signers map (for testing only)
func ResetSignersForTesting() {
	signers = nil
}