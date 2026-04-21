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
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

// generateTestPrivateKey generates a random Ed25519 private key for testing.
func generateTestPrivateKey(t *testing.T) string {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	return hex.EncodeToString(privKey.Seed())
}

// setupTestNode creates a test TSS node with test dependencies.
func setupTestNode(t *testing.T) (*Node, *pushcore.Client, *db.DB) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	// Create a minimal client (will fail on actual gRPC calls, but that's OK for validation tests)
	// The node initialization doesn't require actual client calls, only the config validation
	testClient := &pushcore.Client{}

	cfg := Config{
		ValidatorAddress: "validator1",
		P2PPrivateKeyHex: generateTestPrivateKey(t),
		LibP2PListen:     "/ip4/127.0.0.1/tcp/0",
		HomeDir:          t.TempDir(),
		Password:         "test-password",
		Database:         database,
		PushCore:         testClient,
		Logger:           zerolog.Nop(),
		PollInterval:     100 * time.Millisecond,
		CoordinatorRange: 100,
	}

	node, err := NewNode(context.Background(), cfg)
	require.NoError(t, err)

	return node, testClient, database
}

func TestNewNode_Validation(t *testing.T) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	testClient := &pushcore.Client{}

	tests := []struct {
		name    string
		cfg     Config
		wantErr string
	}{
		{
			name: "missing validator address",
			cfg: Config{
				P2PPrivateKeyHex: generateTestPrivateKey(t),
				HomeDir:          t.TempDir(),
				Database:         database,
				PushCore:         testClient,
			},
			wantErr: "validator address is required",
		},
		{
			name: "missing private key",
			cfg: Config{
				ValidatorAddress: "validator1",
				HomeDir:          t.TempDir(),
				Database:         database,
				PushCore:         testClient,
			},
			wantErr: "private key is required",
		},
		{
			name: "missing home directory",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex: generateTestPrivateKey(t),
				Database:         database,
				PushCore:         testClient,
			},
			wantErr: "home directory is required",
		},
		{
			name: "missing database",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex: generateTestPrivateKey(t),
				HomeDir:          t.TempDir(),
				PushCore:         testClient,
			},
			wantErr: "database is required",
		},
		{
			name: "missing pushCore",
			cfg: Config{
				ValidatorAddress: "validator1",
				P2PPrivateKeyHex: generateTestPrivateKey(t),
				HomeDir:          t.TempDir(),
				Database:         database,
			},
			wantErr: "pushCore is required",
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

func TestConvertPrivateKeyHexToBase64(t *testing.T) {
	t.Run("valid 32-byte hex key", func(t *testing.T) {
		seed := make([]byte, 32)
		_, err := rand.Read(seed)
		require.NoError(t, err)

		hexKey := hex.EncodeToString(seed)
		result, err := convertPrivateKeyHexToBase64(hexKey)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("invalid hex characters", func(t *testing.T) {
		_, err := convertPrivateKeyHexToBase64("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hex decode failed")
	})

	t.Run("wrong length 16 bytes", func(t *testing.T) {
		shortKey := hex.EncodeToString(make([]byte, 16))
		_, err := convertPrivateKeyHexToBase64(shortKey)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wrong key length")
	})

	t.Run("wrong length 64 bytes", func(t *testing.T) {
		longKey := hex.EncodeToString(make([]byte, 64))
		_, err := convertPrivateKeyHexToBase64(longKey)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wrong key length")
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := convertPrivateKeyHexToBase64("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "wrong key length")
	})

	t.Run("key with whitespace is trimmed", func(t *testing.T) {
		seed := make([]byte, 32)
		_, err := rand.Read(seed)
		require.NoError(t, err)

		hexKey := hex.EncodeToString(seed)
		paddedKey := "  " + hexKey + "  \n"

		result, err := convertPrivateKeyHexToBase64(paddedKey)
		require.NoError(t, err)
		assert.NotEmpty(t, result)

		expected, err := convertPrivateKeyHexToBase64(hexKey)
		require.NoError(t, err)
		assert.Equal(t, expected, result)
	})
}

func TestHandleACKMessage_CoordinatorNil(t *testing.T) {
	t.Run("coordinator is nil", func(t *testing.T) {
		node, _, _ := setupTestNode(t)
		// Node is not started, so coordinator is nil
		err := node.HandleACKMessage(context.Background(), "sender-peer", &coordinator.Message{
			Type:    "ack",
			EventID: "event-123",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "coordinator not initialized")
	})

	t.Run("node not started coordinator nil", func(t *testing.T) {
		node, _, _ := setupTestNode(t)
		assert.Nil(t, node.coordinator)
		err := node.HandleACKMessage(context.Background(), "peer-abc", &coordinator.Message{
			Type:    "ack",
			EventID: "event-456",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "coordinator not initialized")
	})
}

func TestNewNode_DefaultValues(t *testing.T) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	testClient := &pushcore.Client{}

	t.Run("defaults are applied when optional fields are zero", func(t *testing.T) {
		cfg := Config{
			ValidatorAddress: "validator1",
			P2PPrivateKeyHex: generateTestPrivateKey(t),
			LibP2PListen:     "/ip4/127.0.0.1/tcp/0",
			HomeDir:          t.TempDir(),
			Password:         "test-password",
			Database:         database,
			PushCore:         testClient,
			Logger:           zerolog.Nop(),
		}

		node, err := NewNode(context.Background(), cfg)
		require.NoError(t, err)

		assert.Equal(t, 10*time.Second, node.coordinatorPollInterval)
		assert.Equal(t, uint64(1000), node.coordinatorRange)
		assert.Equal(t, 2*time.Minute, node.sessionExpiryTime)
		assert.Equal(t, 30*time.Second, node.sessionExpiryCheckInterval)
		assert.Equal(t, uint64(60), node.sessionExpiryBlockDelay)
	})

	t.Run("custom values override defaults", func(t *testing.T) {
		cfg := Config{
			ValidatorAddress:           "validator1",
			P2PPrivateKeyHex:           generateTestPrivateKey(t),
			LibP2PListen:               "/ip4/127.0.0.1/tcp/0",
			HomeDir:                    t.TempDir(),
			Password:                   "test-password",
			Database:                   database,
			PushCore:                   testClient,
			Logger:                     zerolog.Nop(),
			PollInterval:               5 * time.Second,
			CoordinatorRange:           500,
			SessionExpiryTime:          10 * time.Minute,
			SessionExpiryCheckInterval: 1 * time.Minute,
			SessionExpiryBlockDelay:    120,
		}

		node, err := NewNode(context.Background(), cfg)
		require.NoError(t, err)

		assert.Equal(t, 5*time.Second, node.coordinatorPollInterval)
		assert.Equal(t, uint64(500), node.coordinatorRange)
		assert.Equal(t, 10*time.Minute, node.sessionExpiryTime)
		assert.Equal(t, 1*time.Minute, node.sessionExpiryCheckInterval)
		assert.Equal(t, uint64(120), node.sessionExpiryBlockDelay)
	})
}

func TestNode_SendToUnknownPeer(t *testing.T) {
	node, _, _ := setupTestNode(t)
	ctx := context.Background()

	require.NoError(t, node.Start(ctx))
	defer node.Stop()

	err := node.Send(ctx, "12D3KooWFakeUnknownPeerIDxxxxxxxxxxxxxxxxx", []byte("hello"))
	require.Error(t, err)
}
