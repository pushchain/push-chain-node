package tss

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
)

// mockDataProvider is a mock implementation of coordinator.DataProvider for testing.
type mockDataProvider struct {
	latestBlock      uint64
	validators       []*types.UniversalValidator
	currentTSSKeyId  string
	getBlockNumErr   error
	getValidatorsErr error
	getKeyIdErr      error
}

func (m *mockDataProvider) GetLatestBlockNum() (uint64, error) {
	if m.getBlockNumErr != nil {
		return 0, m.getBlockNumErr
	}
	return m.latestBlock, nil
}

func (m *mockDataProvider) GetUniversalValidators() ([]*types.UniversalValidator, error) {
	if m.getValidatorsErr != nil {
		return nil, m.getValidatorsErr
	}
	return m.validators, nil
}

func (m *mockDataProvider) GetCurrentTSSKeyId() (string, error) {
	if m.getKeyIdErr != nil {
		return "", m.getKeyIdErr
	}
	return m.currentTSSKeyId, nil
}

// generateTestPrivateKey generates a random Ed25519 private key for testing.
func generateTestPrivateKey(t *testing.T) string {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return hex.EncodeToString(privKey.Seed())
}

// setupTestNode creates a test TSS node with mock dependencies.
func setupTestNode(t *testing.T) (*Node, *mockDataProvider, *db.DB) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	mockDP := &mockDataProvider{
		latestBlock:     100,
		currentTSSKeyId: "test-key-id",
		validators: []*types.UniversalValidator{
			{
				IdentifyInfo: &types.IdentityInfo{CoreValidatorAddress: "validator1"},
				NetworkInfo: &types.NetworkInfo{
					PeerId:     "peer1",
					MultiAddrs: []string{"/ip4/127.0.0.1/tcp/9001"},
				},
				LifecycleInfo: &types.LifecycleInfo{CurrentStatus: types.UVStatus_UV_STATUS_ACTIVE},
			},
		},
	}

	cfg := Config{
		ValidatorAddress: "validator1",
		P2PPrivateKeyHex:    generateTestPrivateKey(t),
		LibP2PListen:     "/ip4/127.0.0.1/tcp/0",
		HomeDir:          t.TempDir(),
		Password:         "test-password",
		Database:         database,
		DataProvider:     mockDP,
		Logger:           zerolog.Nop(),
		PollInterval:     100 * time.Millisecond,
		CoordinatorRange: 100,
	}

	node, err := NewNode(context.Background(), cfg)
	require.NoError(t, err)

	return node, mockDP, database
}

func TestNewNode_Validation(t *testing.T) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	mockDP := &mockDataProvider{}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "missing validator address",
			cfg: Config{
				P2PPrivateKeyHex: generateTestPrivateKey(t),
				HomeDir:       t.TempDir(),
				Database:      database,
				DataProvider:  mockDP,
			},
			wantErr: "validator address is required",
		},
		{
			name: "missing private key",
			cfg: Config{
				ValidatorAddress: "validator1",
				HomeDir:          t.TempDir(),
				Database:         database,
				DataProvider:     mockDP,
			},
			wantErr: "private key is required",
		},
		{
			name: "missing home directory",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex:    generateTestPrivateKey(t),
				Database:         database,
				DataProvider:     mockDP,
			},
			wantErr: "home directory is required",
		},
		{
			name: "missing database",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex:    generateTestPrivateKey(t),
				HomeDir:          t.TempDir(),
				DataProvider:     mockDP,
			},
			wantErr: "database is required",
		},
		{
			name: "missing data provider",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex:    generateTestPrivateKey(t),
				HomeDir:          t.TempDir(),
				Database:         database,
			},
			wantErr: "data provider is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.cfg.Logger = zerolog.Nop()
			tt.cfg.Password = "test-password"
			_, err := NewNode(context.Background(), tt.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestNode_StartStop(t *testing.T) {
	node, _, _ := setupTestNode(t)
	ctx := context.Background()

	t.Run("start node", func(t *testing.T) {
		err := node.Start(ctx)
		require.NoError(t, err)
		assert.NotEmpty(t, node.PeerID())
		assert.NotEmpty(t, node.ListenAddrs())
	})

	t.Run("stop node", func(t *testing.T) {
		err := node.Stop()
		require.NoError(t, err)
	})

	t.Run("double start", func(t *testing.T) {
		node2, _, _ := setupTestNode(t)
		err := node2.Start(ctx)
		require.NoError(t, err)

		err = node2.Start(ctx)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		node2.Stop()
	})

	t.Run("stop without start", func(t *testing.T) {
		node2, _, _ := setupTestNode(t)
		err := node2.Stop()
		require.NoError(t, err) // Should not error
	})
}

func TestNode_Send(t *testing.T) {
	node, _, _ := setupTestNode(t)
	ctx := context.Background()

	t.Run("send before start", func(t *testing.T) {
		err := node.Send(ctx, "peer1", []byte("test"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "network not initialized")
	})

	t.Run("send to self", func(t *testing.T) {
		require.NoError(t, node.Start(ctx))
		defer node.Stop()

		// Send to self should call onReceive directly
		err := node.Send(ctx, node.PeerID(), []byte("test"))
		require.NoError(t, err)
	})
}

func TestNode_PeerID_ListenAddrs(t *testing.T) {
	node, _, _ := setupTestNode(t)

	t.Run("before start", func(t *testing.T) {
		assert.Empty(t, node.PeerID())
		assert.Nil(t, node.ListenAddrs())
	})

	t.Run("after start", func(t *testing.T) {
		require.NoError(t, node.Start(context.Background()))
		defer node.Stop()

		assert.NotEmpty(t, node.PeerID())
		assert.NotEmpty(t, node.ListenAddrs())
	})
}
