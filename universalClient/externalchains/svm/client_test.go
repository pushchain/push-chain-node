package svm

import (
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// testChainConfig creates a test chain-specific config with RPC URLs.
func testChainConfig(rpcURLs []string) *config.ChainSpecificConfig {
	return &config.ChainSpecificConfig{
		RPCURLs: rpcURLs,
	}
}

// validSVMChainID returns a valid CAIP-2 Solana chain ID for testing.
func validSVMChainID() string {
	return "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"
}

// validChainConfig returns a minimal valid uregistrytypes.ChainConfig for SVM tests.
func validChainConfig() *uregistrytypes.ChainConfig {
	return &uregistrytypes.ChainConfig{
		Chain:  validSVMChainID(),
		VmType: uregistrytypes.VmType_SVM,
	}
}

func TestNewClient_NilConfig(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	client, err := NewClient(nil, nil, nil, nil, "", logger)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "config is nil")
}

func TestNewClient_InvalidVMType(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:  validSVMChainID(),
		VmType: uregistrytypes.VmType_EVM, // wrong VM type
	}

	client, err := NewClient(cfg, nil, nil, nil, "", logger)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "invalid VM type for Solana client")
}

func TestNewClient_InvalidChainID(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:  "eip155:1", // not a solana chain
		VmType: uregistrytypes.VmType_SVM,
	}

	client, err := NewClient(cfg, nil, testChainConfig([]string{"https://rpc.example.com"}), nil, "", logger)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "failed to parse chain ID")
}

func TestNewClient_NoRPCURLs_NilChainConfig(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()

	client, err := NewClient(cfg, nil, nil, nil, "", logger)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "no RPC URLs configured")
}

func TestNewClient_NoRPCURLs_EmptySlice(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()

	client, err := NewClient(cfg, nil, testChainConfig([]string{}), nil, "", logger)
	assert.Error(t, err)
	assert.Nil(t, client)
	assert.Contains(t, err.Error(), "no RPC URLs configured")
}

func TestNewClient_ValidCreation(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:          validSVMChainID(),
		VmType:         uregistrytypes.VmType_SVM,
		GatewayAddress: "SomeGateway111111111111111111111111111111111",
		Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: true},
	}

	chainSpecific := testChainConfig([]string{"https://api.mainnet-beta.solana.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "/tmp/node", logger)
	require.NoError(t, err)
	require.NotNil(t, client)

	assert.Equal(t, validSVMChainID(), client.ChainID())
	assert.Equal(t, cfg, client.GetConfig())
	assert.Equal(t, "EtWTRABZaYq6iMfeYKouRu166VU2xqa1", client.genesisHash)
}

func TestNewClient_WithDatabase(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	require.NotNil(t, database)

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://api.mainnet-beta.solana.com"})

	client, err := NewClient(cfg, database, chainSpecific, nil, "", logger)
	require.NoError(t, err)
	require.NotNil(t, client)
	assert.Equal(t, database, client.database)
}

func TestChainID(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	assert.Equal(t, validSVMChainID(), client.ChainID())
}

func TestGetConfig(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:          validSVMChainID(),
		VmType:         uregistrytypes.VmType_SVM,
		GatewayAddress: "SomeGateway",
	}
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	got := client.GetConfig()
	assert.Equal(t, cfg, got)
	assert.Equal(t, "SomeGateway", got.GatewayAddress)
}

func TestGetTxBuilder_NilBeforeStart(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	txb, err := client.GetTxBuilder()
	assert.Error(t, err)
	assert.Nil(t, txb)
	assert.Contains(t, err.Error(), "txBuilder not available")
}

func TestIsHealthy_NotStarted(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	// rpcClient is nil before Start
	assert.False(t, client.IsHealthy())
}

func TestStop_BeforeStart(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	// Calling Stop before Start should not panic
	err = client.Stop()
	assert.NoError(t, err)
}

func TestStop_CalledTwice(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	// Double stop should be safe
	assert.NoError(t, client.Stop())
	assert.NoError(t, client.Stop())
}

func TestApplyDefaults_AllDefaults(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := validChainConfig()
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	defaults := client.applyDefaults()

	assert.Equal(t, 5, defaults.eventPollingInterval)
	assert.Equal(t, 30, defaults.gasPriceInterval)
	assert.Equal(t, uint64(5), defaults.fastConfirmations)
	assert.Equal(t, uint64(12), defaults.standardConfirmations)
	assert.Equal(t, 0, defaults.gasPriceMarkupPercent)
}

func TestApplyDefaults_EventPollingOverride(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	polling := 15
	chainSpecific := &config.ChainSpecificConfig{
		RPCURLs:                     []string{"https://rpc.example.com"},
		EventPollingIntervalSeconds: &polling,
	}

	cfg := validChainConfig()
	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	defaults := client.applyDefaults()
	assert.Equal(t, 15, defaults.eventPollingInterval)
}

func TestApplyDefaults_GasPriceOverride(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	gasPriceInterval := 60
	gasPriceMarkup := 20
	chainSpecific := &config.ChainSpecificConfig{
		RPCURLs:               []string{"https://rpc.example.com"},
		GasPriceIntervalSeconds: &gasPriceInterval,
		GasPriceMarkupPercent:   &gasPriceMarkup,
	}

	cfg := validChainConfig()
	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	defaults := client.applyDefaults()
	assert.Equal(t, 60, defaults.gasPriceInterval)
	assert.Equal(t, 20, defaults.gasPriceMarkupPercent)
}

func TestApplyDefaults_BlockConfirmationOverride(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:  validSVMChainID(),
		VmType: uregistrytypes.VmType_SVM,
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     3,
			StandardInbound: 20,
		},
	}
	chainSpecific := testChainConfig([]string{"https://rpc.example.com"})

	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	defaults := client.applyDefaults()
	assert.Equal(t, uint64(3), defaults.fastConfirmations)
	assert.Equal(t, uint64(20), defaults.standardConfirmations)
}

func TestApplyDefaults_ZeroValueNotApplied(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Zero values should not override defaults (the code checks > 0)
	zeroPolling := 0
	zeroGasInterval := 0
	zeroMarkup := 0
	chainSpecific := &config.ChainSpecificConfig{
		RPCURLs:                     []string{"https://rpc.example.com"},
		EventPollingIntervalSeconds: &zeroPolling,
		GasPriceIntervalSeconds:     &zeroGasInterval,
		GasPriceMarkupPercent:       &zeroMarkup,
	}

	cfg := validChainConfig()
	client, err := NewClient(cfg, nil, chainSpecific, nil, "", logger)
	require.NoError(t, err)

	defaults := client.applyDefaults()
	// Zero values should not override defaults
	assert.Equal(t, 5, defaults.eventPollingInterval)
	assert.Equal(t, 30, defaults.gasPriceInterval)
	assert.Equal(t, 0, defaults.gasPriceMarkupPercent) // 0 is the default too
}

func TestParseSolanaChainID(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    string
		expectErr   bool
		errContains string
	}{
		{
			name:     "Valid mainnet",
			input:    "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			expected: "EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
		},
		{
			name:     "Valid devnet",
			input:    "solana:8E9rvCKLFQia2Y35HXjjpWzj8weVo44K",
			expected: "8E9rvCKLFQia2Y35HXjjpWzj8weVo44K",
		},
		{
			name:        "Invalid format - no colon",
			input:       "solanaEtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			expectErr:   true,
			errContains: "invalid CAIP-2 format",
		},
		{
			name:        "Invalid format - too many colons",
			input:       "solana:abc:def",
			expectErr:   true,
			errContains: "invalid CAIP-2 format",
		},
		{
			name:        "Wrong namespace",
			input:       "eip155:1",
			expectErr:   true,
			errContains: "not a Solana chain",
		},
		{
			name:        "Empty genesis hash",
			input:       "solana:",
			expectErr:   true,
			errContains: "empty genesis hash",
		},
		{
			name:        "Empty string",
			input:       "",
			expectErr:   true,
			errContains: "invalid CAIP-2 format",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseSolanaChainID(tt.input)

			if tt.expectErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestNewClient_FullConfigGetters(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))

	cfg := &uregistrytypes.ChainConfig{
		Chain:          validSVMChainID(),
		VmType:         uregistrytypes.VmType_SVM,
		GatewayAddress: "GatewayPubkey111111111111111111111111111111111",
		Enabled:        &uregistrytypes.ChainEnabled{IsInboundEnabled: true, IsOutboundEnabled: false},
		BlockConfirmation: &uregistrytypes.BlockConfirmation{
			FastInbound:     2,
			StandardInbound: 15,
		},
	}

	polling := 10
	gasInterval := 45
	gasMarkup := 15
	chainSpecific := &config.ChainSpecificConfig{
		RPCURLs:                     []string{"https://api.mainnet-beta.solana.com"},
		EventPollingIntervalSeconds: &polling,
		GasPriceIntervalSeconds:     &gasInterval,
		GasPriceMarkupPercent:       &gasMarkup,
	}

	client, err := NewClient(cfg, nil, chainSpecific, nil, "/tmp/home", logger)
	require.NoError(t, err)

	// Verify all getters
	assert.Equal(t, validSVMChainID(), client.ChainID())
	assert.Equal(t, cfg, client.GetConfig())
	assert.False(t, client.IsHealthy()) // not started

	_, txErr := client.GetTxBuilder()
	assert.Error(t, txErr)

	// Verify applyDefaults picks up overrides
	defaults := client.applyDefaults()
	assert.Equal(t, 10, defaults.eventPollingInterval)
	assert.Equal(t, 45, defaults.gasPriceInterval)
	assert.Equal(t, 15, defaults.gasPriceMarkupPercent)
	assert.Equal(t, uint64(2), defaults.fastConfirmations)
	assert.Equal(t, uint64(15), defaults.standardConfirmations)

	// Stop should be safe even without Start
	assert.NoError(t, client.Stop())
}
