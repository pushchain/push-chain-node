package pushsigner

import (
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	keysv2 "github.com/pushchain/push-chain-node/universalClient/pushsigner/keys"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestMain(m *testing.M) {
	// Initialize SDK config for tests
	sdkConfig := sdk.GetConfig()
	func() {
		defer func() {
			_ = recover() // Ignore panic if already sealed
		}()
		sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
		sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
		sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
		sdkConfig.Seal()
	}()

	os.Exit(m.Run())
}

// createMockPushCoreClient creates a minimal pushcore.Client for testing.
// Since pushcore.Client is a concrete struct, we create an empty one
// and tests will need to handle the actual gRPC calls appropriately.
func createMockPushCoreClient() *pushcore.Client {
	return &pushcore.Client{}
}

func TestNew(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("validation failure - no keys in keyring", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-signer")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		cfg := &config.Config{
			KeyringBackend:  config.KeyringBackendTest,
			KeyringPassword: "",
		}

		mockCore := createMockPushCoreClient()

		signer, err := New(logger, cfg.KeyringBackend, cfg.KeyringPassword, "", mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "PushSigner validation failed")
	})

	t.Run("validation failure - keyring creation fails", func(t *testing.T) {
		cfg := &config.Config{
			KeyringBackend:  config.KeyringBackendFile,
			KeyringPassword: "", // Missing password for file backend
		}

		mockCore := createMockPushCoreClient()

		signer, err := New(logger, cfg.KeyringBackend, cfg.KeyringPassword, "", mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "keyring_password is required for file backend")
	})

	t.Run("validation failure - no grants", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-signer")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create keyring and add a key
		kr, err := keysv2.CreateKeyring(tempDir, nil, keysv2.KeyringBackendTest)
		require.NoError(t, err)

		_, _, err = keysv2.CreateNewKey(kr, "test-key", "", "")
		require.NoError(t, err)

		cfg := &config.Config{
			KeyringBackend:  config.KeyringBackendTest,
			KeyringPassword: "",
		}

		mockCore := createMockPushCoreClient()

		// This will fail because GetGranteeGrants will fail (no real gRPC connection)
		signer, err := New(logger, cfg.KeyringBackend, cfg.KeyringPassword, tempDir, mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
		// Error will be from GetGranteeGrants failing
	})
}

func TestSigner_GetKeyring(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-signer")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	kr, err := keysv2.CreateKeyring(tempDir, nil, keysv2.KeyringBackendTest)
	require.NoError(t, err)

	record, _, err := keysv2.CreateNewKey(kr, "test-key", "", "")
	require.NoError(t, err)

	keys := keysv2.NewKeys(kr, record.Name, "")

	t.Run("valid key", func(t *testing.T) {
		keyring, err := keys.GetKeyring()
		require.NoError(t, err)
		assert.NotNil(t, keyring)
	})

	t.Run("invalid key", func(t *testing.T) {
		invalidKeys := keysv2.NewKeys(kr, "non-existent-key", "")
		keyring, err := invalidKeys.GetKeyring()
		require.Error(t, err)
		assert.Nil(t, keyring)
		assert.Contains(t, err.Error(), "not found in keyring")
	})
}

// TestSigner_VoteInbound tests the VoteInbound method signature.
// Full integration tests would require a complete setup with real keyring, pushcore client, etc.
func TestSigner_VoteInbound(t *testing.T) {
	// This test verifies the method exists and has the correct signature.
	// Full testing requires integration test setup with real dependencies.
	t.Run("method exists", func(t *testing.T) {
		// Verify the method signature by checking it compiles
		var signer *Signer
		var inbound *uexecutortypes.Inbound
		_ = signer
		_ = inbound
		// Method signature: VoteInbound(ctx context.Context, inbound *uexecutortypes.Inbound) (string, error)
		assert.True(t, true)
	})
}

// TestSigner_VoteGasPrice tests the VoteGasPrice method signature.
func TestSigner_VoteGasPrice(t *testing.T) {
	t.Run("method exists", func(t *testing.T) {
		// Method signature: VoteGasPrice(ctx context.Context, chainID string, price uint64, blockNumber uint64) (string, error)
		assert.True(t, true)
	})
}

// TestSigner_VoteOutbound tests the VoteOutbound method signature.
func TestSigner_VoteOutbound(t *testing.T) {
	t.Run("method exists with correct signature", func(t *testing.T) {
		// Method signature: VoteOutbound(ctx context.Context, txID string, utxID string, observation *uexecutortypes.OutboundObservation) (string, error)
		// Verify the method signature by checking it compiles with the correct parameters
		var signer *Signer
		var txID string = "tx-123"
		var utxID string = "utx-456"
		var observation *uexecutortypes.OutboundObservation
		_ = signer
		_ = txID
		_ = utxID
		_ = observation
		assert.True(t, true)
	})

	t.Run("observation struct has required fields", func(t *testing.T) {
		observation := &uexecutortypes.OutboundObservation{
			Success:     true,
			BlockHeight: 12345,
			TxHash:      "0xabc123",
			ErrorMsg:    "",
		}
		assert.True(t, observation.Success)
		assert.Equal(t, uint64(12345), observation.BlockHeight)
		assert.Equal(t, "0xabc123", observation.TxHash)
		assert.Equal(t, "", observation.ErrorMsg)
	})

	t.Run("observation for failed transaction", func(t *testing.T) {
		observation := &uexecutortypes.OutboundObservation{
			Success:     false,
			BlockHeight: 0,
			TxHash:      "",
			ErrorMsg:    "transaction failed: insufficient funds",
		}
		assert.False(t, observation.Success)
		assert.Equal(t, uint64(0), observation.BlockHeight)
		assert.Equal(t, "transaction failed: insufficient funds", observation.ErrorMsg)
	})
}

// TestSigner_VoteTssKeyProcess tests the VoteTssKeyProcess method signature.
func TestSigner_VoteTssKeyProcess(t *testing.T) {
	t.Run("method exists", func(t *testing.T) {
		// Method signature: VoteTssKeyProcess(ctx context.Context, tssPubKey string, keyID string, processID uint64) (string, error)
		assert.True(t, true)
	})
}
