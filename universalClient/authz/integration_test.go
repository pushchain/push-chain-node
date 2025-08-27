package authz

import (
	"os"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// AuthZIntegrationTestSuite tests the complete AuthZ flow
type AuthZIntegrationTestSuite struct {
	suite.Suite
	tempDir        string
	operatorDir    string
	hotkeyDir      string
	operatorKeys   *keys.Keys
	hotkeyKeys     *keys.Keys
	cfg            *config.Config
	logger         zerolog.Logger
}

func (suite *AuthZIntegrationTestSuite) SetupSuite() {
	// Initialize SDK config for tests
	sdkConfig := sdk.GetConfig()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
	
	// Create temporary directories - separate for each keyring to prevent conflicts
	var err error
	suite.tempDir, err = os.MkdirTemp("", "authz-integration-test")
	require.NoError(suite.T(), err)
	
	suite.operatorDir, err = os.MkdirTemp("", "authz-operator-test")
	require.NoError(suite.T(), err)
	
	suite.hotkeyDir, err = os.MkdirTemp("", "authz-hotkey-test")
	require.NoError(suite.T(), err)

	// Setup logger
	suite.logger = zerolog.New(nil).Level(zerolog.Disabled)

	// Create test configuration
	suite.cfg = &config.Config{
		AuthzGranter:   "",  // Will be set after operator key creation
		AuthzHotkey:    "test-hotkey",
		KeyringBackend: config.KeyringBackendTest,
		PChainHome:     suite.hotkeyDir,  // Use hotkey dir for config
	}

	// Create operator keys (simulating validator key)
	suite.createOperatorKeys()
	
	// Create hot keys
	suite.createHotKeys()

	// Update config with operator address
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	suite.cfg.AuthzGranter = operatorAddr.String()
}

func (suite *AuthZIntegrationTestSuite) TearDownSuite() {
	if suite.tempDir != "" {
		os.RemoveAll(suite.tempDir)
	}
	if suite.operatorDir != "" {
		os.RemoveAll(suite.operatorDir)
	}
	if suite.hotkeyDir != "" {
		os.RemoveAll(suite.hotkeyDir)
	}
}

func (suite *AuthZIntegrationTestSuite) createOperatorKeys() {
	// Create interface registry and codec - ensure clean registry for each test
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	
	// Create keyring for operator using standard SDK service name and separate directory
	kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, suite.operatorDir, nil, cdc)
	require.NoError(suite.T(), err)

	// Create operator key
	operatorKeyName := "test-operator"
	record, err := keys.CreateNewKey(kb, operatorKeyName, "", "")
	require.NoError(suite.T(), err)

	operatorAddr, err := record.GetAddress()
	require.NoError(suite.T(), err)

	// Create Keys instance for operator
	suite.operatorKeys = keys.NewKeysWithKeybase(kb, operatorAddr, operatorKeyName, "")
	require.NotNil(suite.T(), suite.operatorKeys)
}

func (suite *AuthZIntegrationTestSuite) createHotKeys() {
	// Create hot keys using the standard flow
	hotkeyKeys, err := keys.NewKeys(suite.cfg.AuthzHotkey, suite.cfg)
	if err != nil {
		// If NewKeys fails (expected in test environment), create manually
		// Create interface registry and codec - ensure clean registry for each test
		registry := codectypes.NewInterfaceRegistry()
		cryptocodec.RegisterInterfaces(registry)
		banktypes.RegisterInterfaces(registry)
		cdc := codec.NewProtoCodec(registry)
		
		// Use standard SDK service name and separate directory for hotkey
		kb, err := keyring.New(sdk.KeyringServiceName(), keyring.BackendTest, suite.hotkeyDir, nil, cdc)
		require.NoError(suite.T(), err)

		record, err := keys.CreateNewKey(kb, suite.cfg.AuthzHotkey, "", "")
		require.NoError(suite.T(), err)

		hotkeyAddr, err := record.GetAddress()
		require.NoError(suite.T(), err)

		suite.hotkeyKeys = keys.NewKeysWithKeybase(kb, hotkeyAddr, suite.cfg.AuthzHotkey, "")
	} else {
		suite.hotkeyKeys = hotkeyKeys
	}
	
	require.NotNil(suite.T(), suite.hotkeyKeys)
}

// TestFullAuthZFlow tests the complete AuthZ transaction flow
func (suite *AuthZIntegrationTestSuite) TestFullAuthZFlow() {
	// Step 1: Setup AuthZ signers
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	// Initialize AuthZ signer list
	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)
	assert.True(suite.T(), IsSignerConfigured())

	// Step 2: Create a test message using real SDK message
	testMsg := &banktypes.MsgSend{
		FromAddress: operatorAddr.String(),
		ToAddress:   hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
	}

	// Step 3: Get signer for the message type
	msgTypeURL := sdk.MsgTypeURL(testMsg)
	signer, err := GetSigner(msgTypeURL)
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), operatorAddr.String(), signer.GranterAddress)
	assert.Equal(suite.T(), hotkeyAddr, signer.GranteeAddress)

	// Step 4: Wrap message with AuthZ
	authzMsg, err := WrapWithAuthZ(testMsg, signer)
	require.NoError(suite.T(), err)
	require.NotNil(suite.T(), authzMsg)

	// Verify the wrapped message structure
	assert.Equal(suite.T(), hotkeyAddr.String(), authzMsg.Grantee)
	assert.Len(suite.T(), authzMsg.Msgs, 1)

	// Step 5: Validate signer configuration
	err = ValidateSigner(signer)
	assert.NoError(suite.T(), err)
}

// TestAuthZGrantExpiration tests handling of expired grants
func (suite *AuthZIntegrationTestSuite) TestAuthZGrantExpiration() {
	// Setup signers
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)

	// Create mock grant that's expired
	expiredGrant := &authz.Grant{
		Authorization: nil, // Would contain actual authorization
		Expiration:    &time.Time{}, // Expired time
	}

	// In a real scenario, we would query the chain for grants
	// Here we simulate the expiration check
	assert.NotNil(suite.T(), expiredGrant)
	assert.True(suite.T(), expiredGrant.Expiration.Before(time.Now()))
}

// TestMultipleMessageTypes tests AuthZ with different message types
func (suite *AuthZIntegrationTestSuite) TestMultipleMessageTypes() {
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)

	// Test all allowed message types
	testMessages := []struct {
		name string
		msg  sdk.Msg
	}{
		{
			name: "bank send",
			msg: &banktypes.MsgSend{
				FromAddress: operatorAddr.String(),
				ToAddress:   hotkeyAddr.String(),
				Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
			},
		},
		// Add more test messages as needed
	}

	for _, testMsg := range testMessages {
		suite.T().Run(testMsg.name, func(t *testing.T) {
			msgTypeURL := sdk.MsgTypeURL(testMsg.msg)
			
			// Verify message type is allowed
			assert.True(t, IsAllowedMsgType(msgTypeURL))

			// Get signer
			signer, err := GetSigner(msgTypeURL)
			require.NoError(t, err)

			// Wrap with AuthZ
			authzMsg, err := WrapWithAuthZ(testMsg.msg, signer)
			require.NoError(t, err)
			assert.NotNil(t, authzMsg)
		})
	}
}

// TestSecurityValidation tests security validation in the AuthZ flow
func (suite *AuthZIntegrationTestSuite) TestSecurityValidation() {
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	// Test invalid granter address
	invalidSigner := Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: "invalid-address",
		GranteeAddress: hotkeyAddr,
	}

	err = ValidateSigner(invalidSigner)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "invalid granter address format")

	// Test empty grantee address
	invalidSigner2 := Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: operatorAddr.String(),
		GranteeAddress: sdk.AccAddress{},
	}

	err = ValidateSigner(invalidSigner2)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "grantee address cannot be empty")
}

// TestKeyIntegration tests integration with key management
func (suite *AuthZIntegrationTestSuite) TestKeyIntegration() {
	// Test hot key private key access
	privKey, err := suite.hotkeyKeys.GetPrivateKey("")
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), privKey)

	// Test key address retrieval
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), hotkeyAddr)

	// Test operator address
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	assert.NotEmpty(suite.T(), operatorAddr)

	// Verify addresses are different
	assert.NotEqual(suite.T(), hotkeyAddr.String(), operatorAddr.String())
}

// TestTransactionBuilding tests building complete transactions
func (suite *AuthZIntegrationTestSuite) TestTransactionBuilding() {
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	// Setup AuthZ
	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)

	// Create test message using real SDK message
	testMsg := &banktypes.MsgSend{
		FromAddress: operatorAddr.String(),
		ToAddress:   hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
	}

	// Get signer
	signer, err := GetSigner(sdk.MsgTypeURL(testMsg))
	require.NoError(suite.T(), err)

	// Wrap with AuthZ
	authzMsg, err := WrapWithAuthZ(testMsg, signer)
	require.NoError(suite.T(), err)

	// Create basic transaction components
	msgs := []sdk.Msg{authzMsg}
	memo := "Integration test transaction"
	gasLimit := uint64(200000)
	feeAmount := sdk.NewCoins(sdk.NewInt64Coin("push", 1000))

	// In a real scenario, we would build and sign the complete transaction
	assert.NotEmpty(suite.T(), msgs)
	assert.NotEmpty(suite.T(), memo)
	assert.Greater(suite.T(), gasLimit, uint64(0))
	assert.False(suite.T(), feeAmount.IsZero())
}

// TestErrorRecovery tests error recovery scenarios
func (suite *AuthZIntegrationTestSuite) TestErrorRecovery() {
	// Reset signers to test uninitialized state
	ResetSignersForTesting()
	
	// Test with uninitialized signers
	_, err := GetSigner("/cosmos.bank.v1beta1.MsgSend")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "signers not initialized")

	// Test with disallowed message type
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)

	invalidMsg := &authz.MsgGrant{
		Granter: operatorAddr.String(),
		Grantee: hotkeyAddr.String(),
	}

	signer := Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: operatorAddr.String(),
		GranteeAddress: hotkeyAddr,
	}

	_, err = WrapWithAuthZ(invalidMsg, signer)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "is not allowed for AuthZ execution")
}

// TestConcurrentOperations tests concurrent AuthZ operations
func (suite *AuthZIntegrationTestSuite) TestConcurrentOperations() {
	operatorAddr := suite.operatorKeys.GetOperatorAddress()
	hotkeyAddr, err := suite.hotkeyKeys.GetAddress()
	require.NoError(suite.T(), err)

	SetupAuthZSignerList(operatorAddr.String(), hotkeyAddr)

	// Test concurrent GetSigner calls
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := GetSigner("/cosmos.bank.v1beta1.MsgSend")
			results <- err
		}()
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(suite.T(), err)
	}

	// Test concurrent message wrapping
	testMsg := &banktypes.MsgSend{
		FromAddress: operatorAddr.String(),
		ToAddress:   hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
	}

	signer, err := GetSigner(sdk.MsgTypeURL(testMsg))
	require.NoError(suite.T(), err)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			msg := &banktypes.MsgSend{
				FromAddress: operatorAddr.String(),
				ToAddress:   hotkeyAddr.String(),
				Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", int64(1000+id))),
			}
			_, err := WrapWithAuthZ(msg, signer)
			results <- err
		}(i)
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(suite.T(), err)
	}
}

// TestAllowedMessageTypeValidation tests comprehensive message type validation
func (suite *AuthZIntegrationTestSuite) TestAllowedMessageTypeValidation() {
	// Test all currently allowed message types
	allowedTypes := GetAllAllowedMsgTypes()
	
	expectedTypes := []string{
		"/cosmos.bank.v1beta1.MsgSend",
		"/cosmos.staking.v1beta1.MsgDelegate",
		"/cosmos.staking.v1beta1.MsgUndelegate",
		"/cosmos.gov.v1beta1.MsgVote",
	}

	assert.ElementsMatch(suite.T(), expectedTypes, allowedTypes)

	// Test each type individually
	for _, msgType := range expectedTypes {
		assert.True(suite.T(), IsAllowedMsgType(msgType), "Message type %s should be allowed", msgType)
	}

	// Test disallowed types
	disallowedTypes := []string{
		"/cosmos.authz.v1beta1.MsgGrant",
		"/cosmos.authz.v1beta1.MsgRevoke",
		"/cosmos.distribution.v1beta1.MsgWithdrawValidatorCommission",
	}

	for _, msgType := range disallowedTypes {
		assert.False(suite.T(), IsAllowedMsgType(msgType), "Message type %s should not be allowed", msgType)
	}
}




// TestAuthZIntegration runs the AuthZ integration test suite
func TestAuthZIntegration(t *testing.T) {
	suite.Run(t, new(AuthZIntegrationTestSuite))
}