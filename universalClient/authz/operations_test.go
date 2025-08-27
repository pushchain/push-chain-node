package authz

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/rollchains/pchain/universalClient/keys"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// OperationsTestSuite tests UniversalValidatorOperations
type OperationsTestSuite struct {
	suite.Suite
	operations  *UniversalValidatorOperations
	keys        keys.UniversalValidatorKeys
	signer      *Signer
	clientCtx   client.Context
	logger      zerolog.Logger
	operatorAddr sdk.AccAddress
	hotkeyAddr   sdk.AccAddress
}

func (suite *OperationsTestSuite) SetupTest() {
	// Initialize SDK config
	sdkConfig := sdk.GetConfig()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")

	// Create interface registry and codec
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)

	// Create test keyring
	kb := keyring.NewInMemory(cdc)

	// Create test keys
	operatorRecord, err := kb.NewAccount("operator", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about", "", "", hd.Secp256k1)
	require.NoError(suite.T(), err)
	suite.operatorAddr, err = operatorRecord.GetAddress()
	require.NoError(suite.T(), err)

	hotkeyRecord, err := kb.NewAccount("hotkey", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art", "", "", hd.Secp256k1)
	require.NoError(suite.T(), err)
	suite.hotkeyAddr, err = hotkeyRecord.GetAddress()
	require.NoError(suite.T(), err)

	// Create mock keys
	suite.keys = keys.NewKeysWithKeybase(kb, suite.operatorAddr, "hotkey", "")

	// Setup logger (disabled for tests)
	suite.logger = zerolog.New(nil).Level(zerolog.Disabled)

	// Create signer
	suite.signer = &Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: suite.operatorAddr.String(),
		GranteeAddress: suite.hotkeyAddr,
	}

	// Create client context using the proper function to ensure TxConfig is set
	clientConfig := ClientContextConfig{
		ChainID: "test-chain",
		Keys:    suite.keys,
		Logger:  suite.logger,
	}
	
	var err2 error
	suite.clientCtx, err2 = CreateClientContext(clientConfig)
	require.NoError(suite.T(), err2)

	// Create operations
	suite.operations = NewUniversalValidatorOperations(
		suite.keys,
		suite.signer,
		suite.clientCtx,
		suite.logger,
	)
}

// TestNewUniversalValidatorOperations tests the constructor
func (suite *OperationsTestSuite) TestNewUniversalValidatorOperations() {
	ops := NewUniversalValidatorOperations(
		suite.keys,
		suite.signer,
		suite.clientCtx,
		suite.logger,
	)

	assert.NotNil(suite.T(), ops)
	assert.Equal(suite.T(), suite.keys, ops.keys)
	assert.Equal(suite.T(), suite.signer, ops.signer)
	assert.Equal(suite.T(), suite.clientCtx, ops.clientCtx)
	assert.NotNil(suite.T(), ops.txSigner)
}

// TestSubmitObserverVote tests observer vote submission
func (suite *OperationsTestSuite) TestSubmitObserverVote() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Test with valid message type
	validMsg := &banktypes.MsgSend{
		FromAddress: suite.operatorAddr.String(),
		ToAddress:   suite.hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
	}

	// This will fail because we don't have a real gRPC connection
	// but it tests the validation logic
	_, err := suite.operations.SubmitObserverVote(ctx, validMsg)
	
	// Should pass message type validation but fail on transaction signing
	// due to lack of real chain connection
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to sign transaction")
}

// MockInvalidMsg is a mock message type not allowed for AuthZ
type MockInvalidMsg struct {
	operatorAddr sdk.AccAddress
}

func (m *MockInvalidMsg) Reset()         {}
func (m *MockInvalidMsg) String() string { return "" }
func (m *MockInvalidMsg) ProtoMessage()  {}
func (m *MockInvalidMsg) ValidateBasic() error { return nil }
func (m *MockInvalidMsg) GetSigners() []sdk.AccAddress { return []sdk.AccAddress{m.operatorAddr} }

// TestSubmitObserverVoteInvalidMessage tests with invalid message type
func (suite *OperationsTestSuite) TestSubmitObserverVoteInvalidMessage() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Create a mock message that's not allowed
	invalidMsg := &MockInvalidMsg{operatorAddr: suite.operatorAddr}

	_, err := suite.operations.SubmitObserverVote(ctx, invalidMsg)
	
	// Should fail message type validation
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "is not allowed for AuthZ execution")
}

// TestSubmitObservation tests observation submission
func (suite *OperationsTestSuite) TestSubmitObservation() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Test with valid message type
	validMsg := &banktypes.MsgSend{
		FromAddress: suite.operatorAddr.String(),
		ToAddress:   suite.hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 2000)),
	}

	// This will fail because we don't have a real gRPC connection
	_, err := suite.operations.SubmitObservation(ctx, validMsg)
	
	// Should pass message type validation but fail on transaction signing
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to sign transaction")
}

// TestUpdateRegistry tests registry update submission  
func (suite *OperationsTestSuite) TestUpdateRegistry() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Test with valid message type
	validMsg := &banktypes.MsgSend{
		FromAddress: suite.operatorAddr.String(),
		ToAddress:   suite.hotkeyAddr.String(),
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 3000)),
	}

	// This will fail because we don't have a real gRPC connection
	_, err := suite.operations.UpdateRegistry(ctx, validMsg)
	
	// Should pass message type validation but fail on transaction signing
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to sign transaction")
}

// TestSubmitAuthzTransaction tests generic AuthZ transaction submission
func (suite *OperationsTestSuite) TestSubmitAuthzTransaction() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Test with multiple valid messages
	msgs := []sdk.Msg{
		&banktypes.MsgSend{
			FromAddress: suite.operatorAddr.String(),
			ToAddress:   suite.hotkeyAddr.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
		},
		&banktypes.MsgSend{
			FromAddress: suite.operatorAddr.String(),
			ToAddress:   suite.hotkeyAddr.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 2000)),
		},
	}

	memo := "Test batch transaction"

	// This will fail because we don't have a real gRPC connection
	_, err := suite.operations.SubmitAuthzTransaction(ctx, msgs, memo)
	
	// Should pass message type validation but fail on transaction signing
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to sign transaction")
}

// TestSubmitBatchOperations tests batch operation submission
func (suite *OperationsTestSuite) TestSubmitBatchOperations() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Test with multiple valid messages
	msgs := []sdk.Msg{
		&banktypes.MsgSend{
			FromAddress: suite.operatorAddr.String(),
			ToAddress:   suite.hotkeyAddr.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 5000)),
		},
	}

	memo := "Test batch operations"

	// This will fail because we don't have a real gRPC connection
	_, err := suite.operations.SubmitBatchOperations(ctx, msgs, memo)
	
	// Should pass message type validation but fail on transaction signing
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "failed to sign transaction")
}

// TestGetHotKeyInfo tests hot key information retrieval
func (suite *OperationsTestSuite) TestGetHotKeyInfo() {
	info, err := suite.operations.GetHotKeyInfo()
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), info)
	assert.Equal(suite.T(), suite.hotkeyAddr.String(), info.Address)
	assert.Equal(suite.T(), "hotkey", info.KeyName)
	assert.Equal(suite.T(), suite.operatorAddr.String(), info.OperatorAddress)
	assert.Equal(suite.T(), UniversalValidatorHotKey.String(), info.KeyType)
	assert.NotEmpty(suite.T(), info.PublicKey)
}

// TestGetAccountInfo tests account information retrieval
func (suite *OperationsTestSuite) TestGetAccountInfo() {
	ctx := context.Background()

	// This will fail because we don't have a real gRPC connection
	_, err := suite.operations.GetAccountInfo(ctx)
	
	// Should fail due to lack of real chain connection
	assert.Error(suite.T(), err)
}

// TestValidateOperationalReadiness tests operational readiness validation
func (suite *OperationsTestSuite) TestValidateOperationalReadiness() {
	ctx := context.Background()

	err := suite.operations.ValidateOperationalReadiness(ctx)
	
	// Should pass basic validation checks
	assert.NoError(suite.T(), err)
}

// TestSubmitAuthzTransactionWithInvalidMessage tests invalid message handling
func (suite *OperationsTestSuite) TestSubmitAuthzTransactionWithInvalidMessage() {
	// Setup allowed message types
	UseDefaultMsgTypes()

	ctx := context.Background()

	// Create a mock message that's not allowed
	msgs := []sdk.Msg{&MockInvalidMsg{operatorAddr: suite.operatorAddr}}
	memo := "Test invalid message"

	_, err := suite.operations.SubmitAuthzTransaction(ctx, msgs, memo)
	
	// Should fail message type validation
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "is not allowed")
}

// TestHotKeyInfoWithInvalidSigner tests with invalid signer configuration
func (suite *OperationsTestSuite) TestHotKeyInfoWithInvalidSigner() {
	// Create operations with invalid signer (empty addresses)
	invalidSigner := &Signer{
		KeyType:        UniversalValidatorHotKey,
		GranterAddress: "",
		GranteeAddress: sdk.AccAddress{},
	}

	ops := NewUniversalValidatorOperations(
		suite.keys,
		invalidSigner,
		suite.clientCtx,
		suite.logger,
	)

	info, err := ops.GetHotKeyInfo()
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), info)
	// Should still return info from keys, even with invalid signer
	assert.Equal(suite.T(), suite.hotkeyAddr.String(), info.Address)
	assert.Equal(suite.T(), "", info.OperatorAddress) // Empty due to invalid signer
}

// Run the test suite
func TestOperations(t *testing.T) {
	suite.Run(t, new(OperationsTestSuite))
}