package authz

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AuthzConfigTestSuite tests AuthZ configuration validation
type AuthzConfigTestSuite struct {
	suite.Suite
	logger zerolog.Logger
}

func (suite *AuthzConfigTestSuite) SetupTest() {
	// Initialize SDK config
	sdkConfig := sdk.GetConfig()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")

	// Setup logger (disabled for tests)
	suite.logger = zerolog.New(nil).Level(zerolog.Disabled)
}

// TestKeyTypeString tests key type string conversion
func (suite *AuthzConfigTestSuite) TestKeyTypeString() {
	tests := []struct {
		keyType  KeyType
		expected string
	}{
		{UniversalValidatorHotKey, "UniversalValidatorHotKey"},
		{KeyType(99), "Unknown"},
	}

	for _, tt := range tests {
		suite.T().Run(tt.expected, func(t *testing.T) {
			result := tt.keyType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestSignerValidation tests Signer struct validation
func (suite *AuthzConfigTestSuite) TestSignerValidation() {
	validGranteeAddr := sdk.MustAccAddressFromBech32("push1w7ku9j7jezma7mqv7yterhdvxu0wxzv6c6vrlw")

	tests := []struct {
		name        string
		signer      *Signer
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid signer",
			signer: &Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "push1w7ku9j7jezma7mqv7yterhdvxu0wxzv6c6vrlw",
				GranteeAddress: validGranteeAddr,
			},
			expectError: false,
		},
		{
			name: "empty granter address",
			signer: &Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "",
				GranteeAddress: validGranteeAddr,
			},
			expectError: true,
			errorMsg:    "granter address cannot be empty",
		},
		{
			name: "empty grantee address",
			signer: &Signer{
				KeyType:        UniversalValidatorHotKey,
				GranterAddress: "push1w7ku9j7jezma7mqv7yterhdvxu0wxzv6c6vrlw",
				GranteeAddress: sdk.AccAddress{},
			},
			expectError: true,
			errorMsg:    "grantee address cannot be empty",
		},
	}

	for _, tt := range tests {
		suite.T().Run(tt.name, func(t *testing.T) {
			err := ValidateSigner(*tt.signer)
			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestDefaultGasConfig tests default gas configuration
func (suite *AuthzConfigTestSuite) TestDefaultGasConfig() {
	config := GetDefaultGasConfig()
	
	require.NotNil(suite.T(), config)
	assert.Equal(suite.T(), 1.2, config.GasAdjustment)
	assert.Equal(suite.T(), "0push", config.GasPrices)
	assert.Equal(suite.T(), uint64(1000000), config.MaxGas)
}

// TestMsgTypeHandling tests message type configuration
func (suite *AuthzConfigTestSuite) TestMsgTypeHandling() {
	// Test default allowed message types
	UseDefaultMsgTypes()
	assert.True(suite.T(), IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
	assert.False(suite.T(), IsAllowedMsgType("/cosmos.authz.v1beta1.MsgGrant"))
	
	// Test universal validator message types
	UseUniversalValidatorMsgTypes()
	assert.True(suite.T(), IsAllowedMsgType("/push.observer.MsgVoteOnObservedEvent"))
	assert.False(suite.T(), IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
	
	// Test custom message types
	customTypes := []string{"/custom.module.MsgCustom"}
	SetAllowedMsgTypes(customTypes)
	assert.True(suite.T(), IsAllowedMsgType("/custom.module.MsgCustom"))
	assert.False(suite.T(), IsAllowedMsgType("/cosmos.bank.v1beta1.MsgSend"))
	
	// Test getting all allowed types
	allTypes := GetAllAllowedMsgTypes()
	assert.Len(suite.T(), allTypes, 1)
	assert.Equal(suite.T(), "/custom.module.MsgCustom", allTypes[0])
	
	// Test category detection
	UseDefaultMsgTypes()
	assert.Equal(suite.T(), "default", GetMsgTypeCategory())
	
	UseUniversalValidatorMsgTypes()
	assert.Equal(suite.T(), "universal-validator", GetMsgTypeCategory())
	
	SetAllowedMsgTypes(customTypes)
	assert.Equal(suite.T(), "custom", GetMsgTypeCategory())
}

// Run the test suite
func TestAuthzConfig(t *testing.T) {
	suite.Run(t, new(AuthzConfigTestSuite))
}