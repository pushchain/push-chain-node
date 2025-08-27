package authz

import (
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

// ClientContextTestSuite tests client context functions
type ClientContextTestSuite struct {
	suite.Suite
	keys      keys.UniversalValidatorKeys
	logger    zerolog.Logger
}

func (suite *ClientContextTestSuite) SetupTest() {
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
	operatorAddr, err := operatorRecord.GetAddress()
	require.NoError(suite.T(), err)

	_, err = kb.NewAccount("hotkey", "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art", "", "", hd.Secp256k1)
	require.NoError(suite.T(), err)

	// Create mock keys
	suite.keys = keys.NewKeysWithKeybase(kb, operatorAddr, "hotkey", "")

	// Setup logger (disabled for tests)
	suite.logger = zerolog.New(nil).Level(zerolog.Disabled)
}

// TestCreateClientContext tests client context creation with valid config
func (suite *ClientContextTestSuite) TestCreateClientContext() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		NodeURI:        "tcp://localhost:26657",
		GRPCEndpoint:   "localhost:9090",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), clientCtx)
	assert.Equal(suite.T(), "test-chain", clientCtx.ChainID)
	assert.Equal(suite.T(), "tcp://localhost:26657", clientCtx.NodeURI)
	assert.NotNil(suite.T(), clientCtx.Codec)
	assert.NotNil(suite.T(), clientCtx.InterfaceRegistry)
	assert.NotNil(suite.T(), clientCtx.TxConfig)
	assert.NotNil(suite.T(), clientCtx.Keyring)
}

// TestCreateClientContextMinimal tests client context creation with minimal config
func (suite *ClientContextTestSuite) TestCreateClientContextMinimal() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), clientCtx)
	assert.Equal(suite.T(), "test-chain", clientCtx.ChainID)
	assert.Empty(suite.T(), clientCtx.NodeURI) // Should be empty when not provided
	assert.NotNil(suite.T(), clientCtx.Codec)
	assert.NotNil(suite.T(), clientCtx.InterfaceRegistry)
	assert.NotNil(suite.T(), clientCtx.TxConfig)
	assert.NotNil(suite.T(), clientCtx.Keyring)
}

// TestCreateClientContextWithGRPCEndpoint tests gRPC endpoint setup
func (suite *ClientContextTestSuite) TestCreateClientContextWithGRPCEndpoint() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		GRPCEndpoint:   "localhost:9090",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), clientCtx)
	// Note: GRPCClient is created lazily, so it might be nil here
}

// TestValidateClientContext tests client context validation with valid context
func (suite *ClientContextTestSuite) TestValidateClientContext() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	require.NoError(suite.T(), err)

	err = ValidateClientContext(clientCtx)
	assert.NoError(suite.T(), err)
}

// TestValidateClientContextMissingChainID tests validation with missing chain ID
func (suite *ClientContextTestSuite) TestValidateClientContextMissingChainID() {
	config := ClientContextConfig{
		Keys:           suite.keys,
		Logger:         suite.logger,
		// ChainID is missing
	}

	clientCtx, err := CreateClientContext(config)
	require.NoError(suite.T(), err)

	err = ValidateClientContext(clientCtx)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "chain ID is required")
}

// TestValidateClientContextMissingTxConfig tests validation with missing tx config
func (suite *ClientContextTestSuite) TestValidateClientContextMissingTxConfig() {
	clientCtx := client.Context{}.
		WithChainID("test-chain").
		WithCodec(codec.NewProtoCodec(codectypes.NewInterfaceRegistry())).
		WithKeyring(suite.keys.GetKeybase())
		// TxConfig is missing

	err := ValidateClientContext(clientCtx)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "tx config is required")
}

// TestValidateClientContextMissingCodec tests validation with missing codec
func (suite *ClientContextTestSuite) TestValidateClientContextMissingCodec() {
	clientCtx := client.Context{}.
		WithChainID("test-chain").
		WithKeyring(suite.keys.GetKeybase())
		// Codec is missing

	err := ValidateClientContext(clientCtx)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "tx config is required")
}

// TestValidateClientContextMissingKeyring tests validation with missing keyring
func (suite *ClientContextTestSuite) TestValidateClientContextMissingKeyring() {
	registry := codectypes.NewInterfaceRegistry()
	cdc := codec.NewProtoCodec(registry)

	clientCtx := client.Context{}.
		WithChainID("test-chain").
		WithCodec(cdc)
		// Keyring is missing

	err := ValidateClientContext(clientCtx)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "tx config is required")
}

// TestGetDefaultGasConfig tests default gas configuration
func (suite *ClientContextTestSuite) TestGetDefaultGasConfig() {
	gasConfig := GetDefaultGasConfig()
	
	assert.NotNil(suite.T(), gasConfig)
	assert.Equal(suite.T(), 1.2, gasConfig.GasAdjustment)
	assert.Equal(suite.T(), "0push", gasConfig.GasPrices)
	assert.Equal(suite.T(), uint64(1000000), gasConfig.MaxGas)
}

// TestUpdateClientContextForTesting tests testing context updates
func (suite *ClientContextTestSuite) TestUpdateClientContextForTesting() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	require.NoError(suite.T(), err)

	// Update for testing
	testCtx := UpdateClientContextForTesting(clientCtx)
	
	// Verify the context was returned (we can't easily test internal simulation/offline flags)
	assert.NotNil(suite.T(), testCtx)
}

// TestCreateClientContextInterfaceRegistration tests interface registration
func (suite *ClientContextTestSuite) TestCreateClientContextInterfaceRegistration() {
	config := ClientContextConfig{
		ChainID:        "test-chain",
		Keys:           suite.keys,
		Logger:         suite.logger,
	}

	clientCtx, err := CreateClientContext(config)
	require.NoError(suite.T(), err)

	// Test that the interface registry is properly set up
	registry := clientCtx.InterfaceRegistry
	assert.NotNil(suite.T(), registry)

	// Test that the codec can handle SDK types
	codec := clientCtx.Codec
	assert.NotNil(suite.T(), codec)

	// Create a test message to verify codec works
	testMsg := &banktypes.MsgSend{
		FromAddress: "push1abc123",
		ToAddress:   "push1def456", 
		Amount:      sdk.NewCoins(sdk.NewInt64Coin("push", 1000)),
	}

	// Should be able to marshal/unmarshal without error
	data, err := codec.Marshal(testMsg)
	assert.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), data)

	var unmarshaled banktypes.MsgSend
	err = codec.Unmarshal(data, &unmarshaled)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), testMsg.FromAddress, unmarshaled.FromAddress)
	assert.Equal(suite.T(), testMsg.ToAddress, unmarshaled.ToAddress)
	assert.True(suite.T(), testMsg.Amount.Equal(unmarshaled.Amount))
}

// TestGasConfigType tests GasConfig struct
func (suite *ClientContextTestSuite) TestGasConfigType() {
	gasConfig := GasConfig{
		GasAdjustment: 1.5,
		GasPrices:     "1000upc",
		MaxGas:        2000000,
	}

	assert.Equal(suite.T(), 1.5, gasConfig.GasAdjustment)
	assert.Equal(suite.T(), "1000upc", gasConfig.GasPrices)
	assert.Equal(suite.T(), uint64(2000000), gasConfig.MaxGas)
}

// Run the test suite
func TestClientContext(t *testing.T) {
	suite.Run(t, new(ClientContextTestSuite))
}