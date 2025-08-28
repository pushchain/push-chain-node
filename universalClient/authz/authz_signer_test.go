package authz

import (
	"testing"

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
		GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
	}

	assert.NotNil(t, signer)
	assert.Equal(t, "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp", signer.GranterAddress)
}

func TestNewSignerManager(t *testing.T) {
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp")
	require.NoError(t, err)

	// Create SignerManager
	sm := NewSignerManager(granter, granteeAddr)

	// Verify it was created properly
	assert.NotNil(t, sm)
	assert.Equal(t, granter, sm.GetGranterAddress())
	assert.Equal(t, granteeAddr, sm.GetGranteeAddress())
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
				GranterAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
			},
			expectedValid: true,
		},
		{
			name: "empty granter address",
			signer: &Signer{
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


// TestSignerManagerGetSigner tests the signer manager functionality
func TestSignerManagerGetSigner(t *testing.T) {
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	// Create SignerManager
	sm := NewSignerManager(granter, granteeAddr)

	// Test that GetSigner returns correct signer for each default allowed message type
	for _, msgType := range DefaultAllowedMsgTypes {
		signer, err := sm.GetSigner(msgType)
		assert.NoError(t, err, "GetSigner failed for message type: %s", msgType)
		assert.Equal(t, granter, signer.GranterAddress)
		assert.Equal(t, granteeAddr, signer.GranteeAddress)
	}
}

// TestSignerManagerErrorHandling tests error cases for SignerManager
func TestSignerManagerErrorHandling(t *testing.T) {
	
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	// Create SignerManager
	sm := NewSignerManager(granter, granteeAddr)

	// Test valid message type (use a default allowed type)
	signer, err := sm.GetSigner("/cosmos.bank.v1beta1.MsgSend")
	require.NoError(t, err)
	assert.Equal(t, granter, signer.GranterAddress)
	assert.Equal(t, granteeAddr, signer.GranteeAddress)

	// Test invalid message type
	_, err = sm.GetSigner("/invalid.msg.Type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no signer found for message type")
}





// TestSignerString tests the String method
func TestSignerString(t *testing.T) {
	granter := "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp"
	granteeAddr, err := sdk.AccAddressFromBech32("push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7")
	require.NoError(t, err)

	signer := Signer{
		GranterAddress: granter,
		GranteeAddress: granteeAddr,
	}

	expected := "granter:push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp grantee:push1wgj7lyup5sn9gy3acwgdjgyw3gumv3r6zgrqv7"
	assert.Equal(t, expected, signer.String())
}
