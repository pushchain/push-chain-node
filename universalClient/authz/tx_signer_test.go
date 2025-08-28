package authz

import (
	"context"
	"fmt"
	"testing"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

// MockUniversalValidatorKeys is a mock implementation of the keys interface
type MockUniversalValidatorKeys struct {
	mock.Mock
}

func (m *MockUniversalValidatorKeys) GetAddress() (sdk.AccAddress, error) {
	args := m.Called()
	return args.Get(0).(sdk.AccAddress), args.Error(1)
}

func (m *MockUniversalValidatorKeys) GetPrivateKey(password string) (cryptotypes.PrivKey, error) {
	args := m.Called(password)
	return args.Get(0).(cryptotypes.PrivKey), args.Error(1)
}

func (m *MockUniversalValidatorKeys) GetHotkeyPassword() string {
	args := m.Called()
	return args.String(0)
}


// MockTxConfig is a mock implementation of client.TxConfig
type MockTxConfig struct {
	mock.Mock
}

func (m *MockTxConfig) TxEncoder() sdk.TxEncoder {
	args := m.Called()
	return args.Get(0).(sdk.TxEncoder)
}

func (m *MockTxConfig) TxDecoder() sdk.TxDecoder {
	args := m.Called()
	return args.Get(0).(sdk.TxDecoder)
}

func (m *MockTxConfig) TxJSONEncoder() sdk.TxEncoder {
	args := m.Called()
	return args.Get(0).(sdk.TxEncoder)
}

func (m *MockTxConfig) TxJSONDecoder() sdk.TxDecoder {
	args := m.Called()
	return args.Get(0).(sdk.TxDecoder)
}

func (m *MockTxConfig) NewTxBuilder() client.TxBuilder {
	args := m.Called()
	return args.Get(0).(client.TxBuilder)
}

func (m *MockTxConfig) WrapTxBuilder(newTx sdk.Tx) (client.TxBuilder, error) {
	args := m.Called(newTx)
	return args.Get(0).(client.TxBuilder), args.Error(1)
}

func (m *MockTxConfig) SignModeHandler() *txsigning.HandlerMap {
	return nil
}

func (m *MockTxConfig) SigningContext() *txsigning.Context {
	return nil
}

// Test helper functions to reduce redundancy

// setupTestTxSigner creates a TxSigner with common test setup
func setupTestTxSigner() (*TxSigner, *MockUniversalValidatorKeys, sdk.AccAddress) {
	mockKeys := &MockUniversalValidatorKeys{}
	granteeAddr := sdk.MustAccAddressFromBech32("push1w7ku9j7jezma7mqv7yterhdvxu0wxzv6c6vrlw")
	
	mockKeys.On("GetAddress").Return(granteeAddr, nil)
	
	signerManager := NewSignerManager("push1granter", granteeAddr)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	mockTxConfig := &MockTxConfig{}
	clientCtx := client.Context{}.WithTxConfig(mockTxConfig).WithCodec(cdc)
	logger := zerolog.New(nil)
	
	txSigner := NewTxSigner(mockKeys, signerManager, clientCtx, logger)
	return txSigner, mockKeys, granteeAddr
}

// setupTestTxSignerWithTxConfig creates a TxSigner with custom TxConfig
func setupTestTxSignerWithTxConfig(mockTxConfig *MockTxConfig) (*TxSigner, *MockUniversalValidatorKeys, sdk.AccAddress) {
	mockKeys := &MockUniversalValidatorKeys{}
	granteeAddr := sdk.MustAccAddressFromBech32("push1w7ku9j7jezma7mqv7yterhdvxu0wxzv6c6vrlw")
	
	mockKeys.On("GetAddress").Return(granteeAddr, nil)
	
	signerManager := NewSignerManager("push1granter", granteeAddr)
	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	clientCtx := client.Context{}.WithTxConfig(mockTxConfig).WithCodec(cdc)
	logger := zerolog.New(nil)
	
	txSigner := NewTxSigner(mockKeys, signerManager, clientCtx, logger)
	return txSigner, mockKeys, granteeAddr
}

// setupTestTxBuilder creates a mock TxBuilder with common setup
func setupTestTxBuilder() (*MockTxBuilder, *MockTxConfig) {
	mockTxBuilder := &MockTxBuilder{}
	mockTxConfig := &MockTxConfig{}
	mockTxConfig.On("NewTxBuilder").Return(mockTxBuilder)
	
	mockTxBuilder.On("SetMsgs", mock.Anything).Return(nil)
	mockTxBuilder.On("SetMemo", mock.Anything)
	mockTxBuilder.On("SetGasLimit", mock.Anything)
	mockTxBuilder.On("SetFeeAmount", mock.Anything)
	
	return mockTxBuilder, mockTxConfig
}

func (m *MockTxConfig) MarshalSignatureJSON([]signingtypes.SignatureV2) ([]byte, error) {
	return []byte("{}"), nil
}

func (m *MockTxConfig) UnmarshalSignatureJSON([]byte) ([]signingtypes.SignatureV2, error) {
	return []signingtypes.SignatureV2{}, nil
}

// MockClientContext is a mock implementation of client.Context
type MockClientContext struct {
	mock.Mock
	TxCfg client.TxConfig
}

func (m *MockClientContext) BroadcastTx(txBytes []byte) (*sdk.TxResponse, error) {
	args := m.Called(txBytes)
	return args.Get(0).(*sdk.TxResponse), args.Error(1)
}

func (m *MockClientContext) TxConfig() client.TxConfig {
	return m.TxCfg
}

// MockTxBuilder is a mock implementation of client.TxBuilder
type MockTxBuilder struct {
	mock.Mock
}

func (m *MockTxBuilder) GetTx() signing.Tx {
	args := m.Called()
	return args.Get(0).(signing.Tx)
}

func (m *MockTxBuilder) SetMsgs(msgs ...sdk.Msg) error {
	args := m.Called(msgs)
	return args.Error(0)
}

func (m *MockTxBuilder) SetMemo(memo string) {
	m.Called(memo)
}

func (m *MockTxBuilder) SetFeeAmount(amount sdk.Coins) {
	m.Called(amount)
}

func (m *MockTxBuilder) SetGasLimit(limit uint64) {
	m.Called(limit)
}

func (m *MockTxBuilder) SetTimeoutHeight(height uint64) {
	m.Called(height)
}

func (m *MockTxBuilder) SetFeeGranter(feeGranter sdk.AccAddress) {
	m.Called(feeGranter)
}

func (m *MockTxBuilder) SetFeePayer(feePayer sdk.AccAddress) {
	m.Called(feePayer)
}

func (m *MockTxBuilder) SetSignatures(signatures ...signingtypes.SignatureV2) error {
	args := m.Called(signatures)
	return args.Error(0)
}

func (m *MockTxBuilder) AddAuxSignerData(aux tx.AuxSignerData) error {
	args := m.Called(aux)
	return args.Error(0)
}



func TestNewTxSigner(t *testing.T) {
	txSigner, mockKeys, _ := setupTestTxSigner()

	assert.NotNil(t, txSigner)
	assert.Equal(t, mockKeys, txSigner.keys)
	assert.NotNil(t, txSigner.signerManager)
	assert.NotNil(t, txSigner.txConfig)
}

func TestTxSigner_WrapMessagesWithAuthZ(t *testing.T) {
	txSigner, _, granteeAddr := setupTestTxSigner()

	tests := []struct {
		name        string
		msgs        []sdk.Msg
		expectError bool
		errorMsg    string
	}{
		{
			name:        "empty messages",
			msgs:        []sdk.Msg{},
			expectError: true,
			errorMsg:    "no messages to wrap",
		},
		{
			name: "valid allowed message",
			msgs: []sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: "push1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
					ToAddress:   granteeAddr.String(),
					Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
				},
			},
			expectError: false,
		},
		{
			name: "disallowed message type",
			msgs: []sdk.Msg{
				&authz.MsgGrant{
					Granter: "push1fl48vsnmsdzcv85q5d2q4z5ajdha8yu34mf0eh",
					Grantee: granteeAddr.String(),
				},
			},
			expectError: true,
			errorMsg:    "is not allowed for AuthZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := txSigner.WrapMessagesWithAuthZ(tt.msgs)

			if tt.expectError {
				assert.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Len(t, result, len(tt.msgs))

				// Check that messages are wrapped with MsgExec
				for _, msg := range result {
					execMsg, ok := msg.(*authz.MsgExec)
					assert.True(t, ok, "Message should be wrapped with MsgExec")
					assert.Equal(t, granteeAddr.String(), execMsg.Grantee)
				}
			}
		})
	}
}

func TestTxSigner_CreateTxBuilder(t *testing.T) {
	mockTxBuilder, mockTxConfig := setupTestTxBuilder()
	txSigner, _, granteeAddr := setupTestTxSignerWithTxConfig(mockTxConfig)

	msgs := []sdk.Msg{
		&authz.MsgExec{
			Grantee: granteeAddr.String(),
			Msgs:    []*codectypes.Any{},
		},
	}

	memo := "test memo"
	gasLimit := uint64(200000)
	feeAmount := sdk.NewCoins(sdk.NewInt64Coin("push", 1000))

	result, err := txSigner.CreateTxBuilder(msgs, memo, gasLimit, feeAmount)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, mockTxBuilder, result)

	// Verify all mock expectations were met
	mockTxConfig.AssertExpectations(t)
	mockTxBuilder.AssertExpectations(t)
}

func TestTxSigner_ValidateMessages(t *testing.T) {
	txSigner, _, granteeAddr := setupTestTxSigner()

	tests := []struct {
		name        string
		msgs        []sdk.Msg
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid allowed messages",
			msgs: []sdk.Msg{
				&banktypes.MsgSend{
					FromAddress: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp",
					ToAddress:   granteeAddr.String(),
					Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
				},
			},
			expectError: false,
		},
		{
			name: "all invalid messages",
			msgs: []sdk.Msg{
				&authz.MsgGrant{Granter: "push1z7n2ahw28fveuaqra93nnd2x8ulrw9lkwg3tpp", Grantee: granteeAddr.String()},
			},
			expectError: true,
			errorMsg:    "at index 0 is not allowed for AuthZ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test validation through WrapMessagesWithAuthZ
			_, err := txSigner.WrapMessagesWithAuthZ(tt.msgs)

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

// TestTxSigner_SignAndBroadcastAuthZTx tests transaction signing and broadcasting
func TestTxSigner_SignAndBroadcastAuthZTx(t *testing.T) {
	_, mockTxConfig := setupTestTxBuilder()
	txSigner, _, granteeAddr := setupTestTxSignerWithTxConfig(mockTxConfig)

	msgs := []sdk.Msg{
		&banktypes.MsgSend{
			FromAddress: "push1granter",
			ToAddress:   granteeAddr.String(),
			Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
		},
	}

	// This will fail due to missing gRPC connection but tests the flow
	ctx := context.Background()
	_, err := txSigner.SignAndBroadcastAuthZTx(ctx, msgs, "test memo", 200000, sdk.NewCoins(sdk.NewInt64Coin("push", 1000)))
	
	// Should fail on signing due to no real implementation
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to sign transaction")
}

// TestTxSigner_SignTx tests transaction signing
func TestTxSigner_SignTx(t *testing.T) {
	txSigner, mockKeys, _ := setupTestTxSigner()
	
	// Set up additional mock expectations for SignTx
	mockKeys.On("GetHotkeyPassword").Return("")
	mockKeys.On("GetPrivateKey", "").Return(nil, fmt.Errorf("mock private key error"))
	
	mockTxBuilder := &MockTxBuilder{}

	// This will fail due to missing account info or gRPC connection
	err := txSigner.SignTx(mockTxBuilder)
	
	// Should fail due to missing account info or gRPC connection
	assert.Error(t, err)
}


// TestTxSigner_ErrorScenarios tests various error scenarios
func TestTxSigner_ErrorScenarios(t *testing.T) {
	mockTxConfig := &MockTxConfig{}
	txSigner, _, _ := setupTestTxSignerWithTxConfig(mockTxConfig)

	t.Run("CreateTxBuilder with nil messages", func(t *testing.T) {
		mockTxBuilder := &MockTxBuilder{}
		mockTxConfig.On("NewTxBuilder").Return(mockTxBuilder).Once()
		mockTxBuilder.On("SetMsgs", mock.Anything).Return(nil).Once()
		mockTxBuilder.On("SetMemo", "").Once()
		mockTxBuilder.On("SetGasLimit", uint64(0)).Once()
		mockTxBuilder.On("SetFeeAmount", sdk.NewCoins()).Once()

		result, err := txSigner.CreateTxBuilder(nil, "", 0, sdk.NewCoins())
		assert.NoError(t, err)
		assert.NotNil(t, result)

		mockTxConfig.AssertExpectations(t)
		mockTxBuilder.AssertExpectations(t)
	})
}



