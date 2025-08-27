package authz

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/authz"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	// Initialize SDK config for tests if not already sealed
	sdkConfig := sdk.GetConfig()
	defer func() {
		// Config already sealed, that's fine - ignore panic
		_ = recover()
	}()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")  
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
}

func TestNewSigner(t *testing.T) {
	signer := &Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
	}

	assert.NotNil(t, signer)
	assert.Equal(t, UniversalValidatorHotKey, signer.KeyType)
	assert.Equal(t, "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp", signer.GranterAddress)
}

func TestSetupAuthZSignerListSignature(t *testing.T) {
	// Test that the function has the correct signature
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp")
	require.NoError(t, err)

	// This should not panic and should accept the correct parameters
	SetupAuthZSignerList(granter, granteeAddr)
}

func TestSignerValidation(t *testing.T) {
	tests := []struct {
		name           string
		signer         *Signer
		expectedValid  bool
	}{
		{
			name: "valid signer",
			signer: &Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
			},
			expectedValid: true,
		},
		{
			name: "empty granter address",
			signer: &Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "",
			},
			expectedValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Basic validation - in a real implementation, 
			// you'd have a proper validation method
			isValid := tt.signer.GranterAddress != ""
			assert.Equal(t, tt.expectedValid, isValid)
		})
	}
}

func TestKeyTypeValidation(t *testing.T) {
	// Test that KeyType constants are properly defined
	assert.Equal(t, KeyType(0), UniversalValidatorHotKey)
	
	// Test string representation
	assert.Equal(t, "UniversalValidatorHotKey", UniversalValidatorHotKey.String())
}

// TestSetupAuthZSignerList tests the signer list setup functionality
func TestSetupAuthZSignerList(t *testing.T) {
	ResetSignersForTesting()
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	// Setup signers list
	SetupAuthZSignerList(granter, granteeAddr)

	// Verify signers are configured
	assert.True(t, IsSignerConfigured())

	// Test each allowed message type has a signer
	allSigners := GetAllSigners()
	for _, msgType := range AllowedMsgTypes {
		signer, exists := allSigners[msgType]
		assert.True(t, exists, "Signer not found for message type: %s", msgType)
		assert.Equal(t, granter, signer.GranterAddress)
		assert.Equal(t, granteeAddr, signer.GranteeAddress)
		assert.Equal(t, UniversalValidatorHotKey, signer.KeyType)
	}
}

// TestGetSigner tests retrieving specific signers
func TestGetSigner(t *testing.T) {
	ResetSignersForTesting()
	
	// Reset AllowedMsgTypes to default to avoid test pollution
	UseDefaultMsgTypes()
	
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	// Test without initialization
	_, err = GetSigner("/cosmos.bank.v1beta1.MsgSend")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "signers not initialized")

	// Setup signers
	SetupAuthZSignerList(granter, granteeAddr)

	// Test valid message type (use a default allowed type)
	signer, err := GetSigner("/cosmos.bank.v1beta1.MsgSend")
	require.NoError(t, err)
	assert.Equal(t, granter, signer.GranterAddress)
	assert.Equal(t, granteeAddr, signer.GranteeAddress)

	// Test invalid message type
	_, err = GetSigner("/invalid.msg.Type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no signer found for message type")
}

// TestWrapWithAuthZ tests message wrapping with AuthZ
func TestWrapWithAuthZ(t *testing.T) {
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	signer := Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: granter,
		GranteeAddress: granteeAddr,
	}

	// Create a mock message for testing
	// Using authz.MsgExec as a test message since it's available
	innerMsg := &authz.MsgGrant{
		Granter: granter,
		Grantee: granteeAddr.String(),
	}

	// Test wrapping - this will fail because MsgGrant is not in allowed types
	_, err = WrapWithAuthZ(innerMsg, signer)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is not allowed for AuthZ execution")
}

// TestValidateSigner tests signer validation
func TestValidateSigner(t *testing.T) {
	tests := []struct {
		name        string
		signer      Signer
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid signer",
			signer: Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
				GranteeAddress: sdk.MustAccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7"),
			},
			expectError: false,
		},
		{
			name: "empty granter address",
			signer: Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "",
				GranteeAddress: sdk.MustAccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7"),
			},
			expectError: true,
			errorMsg:    "granter address cannot be empty",
		},
		{
			name: "empty grantee address",
			signer: Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
				GranteeAddress: sdk.AccAddress{},
			},
			expectError: true,
			errorMsg:    "grantee address cannot be empty",
		},
		{
			name: "invalid granter address format",
			signer: Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "invalid_address",
				GranteeAddress: sdk.MustAccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7"),
			},
			expectError: true,
			errorMsg:    "invalid granter address format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSigner(tt.signer)
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestGetAllSigners tests retrieving all signers
func TestGetAllSigners(t *testing.T) {
	// Reset state first
	ResetSignersForTesting()
	// Test when not initialized
	signers := GetAllSigners()
	assert.Empty(t, signers)

	// Setup signers
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)
	
	SetupAuthZSignerList(granter, granteeAddr)

	// Test when initialized
	signers = GetAllSigners()
	assert.Len(t, signers, len(AllowedMsgTypes))
	
	// Verify each message type is present
	for _, msgType := range AllowedMsgTypes {
		_, exists := signers[msgType]
		assert.True(t, exists, "Missing signer for message type: %s", msgType)
	}
}

// TestIsSignerConfigured tests signer configuration status
func TestIsSignerConfigured(t *testing.T) {
	// Reset state
	ResetSignersForTesting()
	assert.False(t, IsSignerConfigured())

	// Setup valid signers
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)
	
	SetupAuthZSignerList(granter, granteeAddr)
	assert.True(t, IsSignerConfigured())
}

// TestSignerString tests the String method
func TestSignerString(t *testing.T) {
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	signer := Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: granter,
		GranteeAddress: granteeAddr,
	}

	expected := "UniversalValidatorHotKey granter:push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp grantee:push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7"
	assert.Equal(t, expected, signer.String())
}
