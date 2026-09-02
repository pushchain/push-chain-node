package pushcore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"testing"

	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestNew(t *testing.T) {
	logger := zerolog.Nop()

	tests := []struct {
		name    string
		urls    []string
		wantErr bool
		errMsg  string
	}{
		{
			name:    "empty URLs list",
			urls:    []string{},
			wantErr: true,
			errMsg:  "at least one gRPC URL is required",
		},
		{
			name:    "nil URLs list",
			urls:    nil,
			wantErr: true,
			errMsg:  "at least one gRPC URL is required",
		},
		{
			name:    "valid URL without port",
			urls:    []string{"localhost"},
			wantErr: false,
		},
		{
			name:    "valid URL with port",
			urls:    []string{"localhost:9090"},
			wantErr: false,
		},
		{
			name:    "http URL",
			urls:    []string{"http://localhost:9090"},
			wantErr: false,
		},
		{
			name:    "https URL",
			urls:    []string{"https://localhost:9090"},
			wantErr: false,
		},
		{
			name:    "multiple URLs",
			urls:    []string{"localhost:9090", "localhost:9091", "localhost:9092"},
			wantErr: false,
		},
		{
			name:    "mix of valid and invalid URLs",
			urls:    []string{"localhost:9090", "invalid-url-that-will-fail:99999", "localhost:9091"},
			wantErr: false, // Should succeed if at least one works
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.urls, logger)

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
				assert.Nil(t, client)
			} else {
				// In test environment, connections might fail
				if err != nil {
					// If all connections failed, that's expected in test env
					assert.Contains(t, err.Error(), "all dials failed")
					assert.Nil(t, client)
				} else {
					require.NotNil(t, client)
					assert.NotNil(t, client.logger)
					// Verify authz and auth clients are initialized
					assert.Equal(t, len(client.conns), len(client.authzClients))
					assert.Equal(t, len(client.conns), len(client.authClients))
					_ = client.Close()
				}
			}
		})
	}
}

func TestClient_Close(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("close with no connections", func(t *testing.T) {
		client := &Client{
			logger: logger,
			conns:  nil,
		}

		err := client.Close()
		assert.NoError(t, err)
		assert.Nil(t, client.conns)
	})

	t.Run("close with connections", func(t *testing.T) {
		client, err := New([]string{"localhost:9090"}, logger)
		if err != nil {
			// If connection fails, create a mock client
			client = &Client{
				logger: logger,
				conns:  []*grpc.ClientConn{},
			}
		}

		err = client.Close()
		assert.NoError(t, err)
		assert.Nil(t, client.conns)
	})
}

func TestClient_GetAllChainConfigs(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger: logger,
			eps:    []uregistrytypes.QueryClient{},
		}

		configs, err := client.GetAllChainConfigs(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, configs)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockRegistryQueryClient{
			allChainConfigsResp: &uregistrytypes.QueryAllChainConfigsResponse{
				Configs: []*uregistrytypes.ChainConfig{
					{Chain: "eip155:1"},
					{Chain: "eip155:84532"},
				},
			},
		}

		client := &Client{
			logger: logger,
			eps:    []uregistrytypes.QueryClient{mockClient},
		}

		configs, err := client.GetAllChainConfigs(ctx)
		require.NoError(t, err)
		require.Len(t, configs, 2)
		assert.Equal(t, "eip155:1", configs[0].Chain)
	})

	t.Run("round robin failover", func(t *testing.T) {
		failingClient := &mockRegistryQueryClient{err: assert.AnError}
		successClient := &mockRegistryQueryClient{
			allChainConfigsResp: &uregistrytypes.QueryAllChainConfigsResponse{
				Configs: []*uregistrytypes.ChainConfig{
					{Chain: "eip155:1"},
				},
			},
		}

		client := &Client{
			logger: logger,
			eps:    []uregistrytypes.QueryClient{failingClient, successClient},
		}

		configs, err := client.GetAllChainConfigs(ctx)
		require.NoError(t, err)
		require.Len(t, configs, 1)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			eps: []uregistrytypes.QueryClient{
				&mockRegistryQueryClient{err: assert.AnError},
				&mockRegistryQueryClient{err: assert.AnError},
			},
		}

		configs, err := client.GetAllChainConfigs(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed on all 2 endpoints")
		assert.Nil(t, configs)
	})
}

func TestClient_GetLatestBlock(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:     logger,
			cmtClients: []cmtservice.ServiceClient{},
		}

		blockNum, err := client.GetLatestBlock(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Equal(t, uint64(0), blockNum)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockCometBFTServiceClient{
			getLatestBlockResp: &cmtservice.GetLatestBlockResponse{
				SdkBlock: &cmtservice.Block{
					Header: cmtservice.Header{
						Height: 12345,
					},
				},
			},
		}

		client := &Client{
			logger:     logger,
			cmtClients: []cmtservice.ServiceClient{mockClient},
		}

		blockNum, err := client.GetLatestBlock(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(12345), blockNum)
	})

	t.Run("nil SdkBlock error", func(t *testing.T) {
		mockClient := &mockCometBFTServiceClient{
			getLatestBlockResp: &cmtservice.GetLatestBlockResponse{
				SdkBlock: nil,
			},
		}

		client := &Client{
			logger:     logger,
			cmtClients: []cmtservice.ServiceClient{mockClient},
		}

		blockNum, err := client.GetLatestBlock(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "SdkBlock is nil")
		assert.Equal(t, uint64(0), blockNum)
	})
}

func TestClient_GetAllUniversalValidators(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:            logger,
			uvalidatorClients: []uvalidatortypes.QueryClient{},
		}

		validators, err := client.GetAllUniversalValidators(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, validators)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockUValidatorQueryClient{
			allUniversalValidatorsResp: &uvalidatortypes.QueryUniversalValidatorsSetResponse{
				UniversalValidator: []*uvalidatortypes.UniversalValidator{
					{},
					{},
				},
			},
		}

		client := &Client{
			logger:            logger,
			uvalidatorClients: []uvalidatortypes.QueryClient{mockClient},
		}

		validators, err := client.GetAllUniversalValidators(context.Background())
		require.NoError(t, err)
		require.Len(t, validators, 2)
	})
}

func TestClient_GetCurrentKey(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{},
		}

		key, err := client.GetCurrentKey(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, key)
	})

	t.Run("successful query with key", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			currentKeyResp: &utsstypes.QueryCurrentKeyResponse{
				Key: &utsstypes.TssKey{
					KeyId: "key-123",
				},
			},
		}

		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{mockClient},
		}

		key, err := client.GetCurrentKey(context.Background())
		require.NoError(t, err)
		require.NotNil(t, key)
		assert.Equal(t, "key-123", key.KeyId)
	})

	t.Run("no key exists (nil key)", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			currentKeyResp: &utsstypes.QueryCurrentKeyResponse{
				Key: nil,
			},
		}

		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{mockClient},
		}

		key, err := client.GetCurrentKey(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no TSS key found")
		assert.Nil(t, key)
	})
}

func TestClient_GetGasPrice(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, price)
	})

	t.Run("empty chainID", func(t *testing.T) {
		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{&mockUExecutorQueryClient{}},
		}

		price, err := client.GetGasPrice(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "chainID is required")
		assert.Nil(t, price)
	})

	t.Run("successful gas price retrieval", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{"validator1", "validator2", "validator3"},
					Prices:          []uint64{1000000000, 2000000000, 3000000000},
					BlockNums:       []uint64{100, 101, 102},
					MedianIndex:     1, // Median is 2 gwei (index 1)
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		require.NotNil(t, price)
		assert.Equal(t, big.NewInt(2000000000), price)
	})

	t.Run("single validator price", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:1",
					Signers:         []string{"validator1"},
					Prices:          []uint64{5000000000},
					BlockNums:       []uint64{100},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:1")
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(5000000000), price)
	})

	t.Run("empty prices array", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{},
					Prices:          []uint64{},
					BlockNums:       []uint64{},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no gas prices available")
		assert.Nil(t, price)
	})

	t.Run("median index out of bounds fallback", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Signers:         []string{"validator1"},
					Prices:          []uint64{1500000000},
					BlockNums:       []uint64{100},
					MedianIndex:     99, // Out of bounds
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		// Should fallback to first price
		assert.Equal(t, big.NewInt(1500000000), price)
	})

	t.Run("round robin failover", func(t *testing.T) {
		failingClient := &mockUExecutorQueryClient{err: assert.AnError}
		successClient := &mockUExecutorQueryClient{
			gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
				GasPrice: &uexecutortypes.GasPrice{
					ObservedChainId: "eip155:84532",
					Prices:          []uint64{1000000000},
					MedianIndex:     0,
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{failingClient, successClient},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.NoError(t, err)
		assert.Equal(t, big.NewInt(1000000000), price)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			uexecutorClients: []uexecutortypes.QueryClient{
				&mockUExecutorQueryClient{err: assert.AnError},
				&mockUExecutorQueryClient{err: assert.AnError},
			},
		}

		price, err := client.GetGasPrice(ctx, "eip155:84532")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed on all 2 endpoints")
		assert.Nil(t, price)
	})
}

func TestClient_GetGranteeGrants(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:       logger,
			authzClients: []authz.QueryClient{},
		}

		grants, err := client.GetGranteeGrants(context.Background(), "cosmos1abc...")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, grants)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockAuthzQueryClient{
			granteeGrantsResp: &authz.QueryGranteeGrantsResponse{
				Grants: []*authz.GrantAuthorization{
					{Granter: "push1granter"},
				},
			},
		}

		client := &Client{
			logger:       logger,
			authzClients: []authz.QueryClient{mockClient},
		}

		grants, err := client.GetGranteeGrants(context.Background(), "cosmos1abc...")
		require.NoError(t, err)
		require.Len(t, grants.Grants, 1)
		assert.Equal(t, "push1granter", grants.Grants[0].Granter)
	})

	t.Run("round robin failover", func(t *testing.T) {
		failingClient := &mockAuthzQueryClient{err: assert.AnError}
		successClient := &mockAuthzQueryClient{
			granteeGrantsResp: &authz.QueryGranteeGrantsResponse{
				Grants: []*authz.GrantAuthorization{},
			},
		}

		client := &Client{
			logger:       logger,
			authzClients: []authz.QueryClient{failingClient, successClient},
		}

		grants, err := client.GetGranteeGrants(context.Background(), "cosmos1abc...")
		require.NoError(t, err)
		assert.NotNil(t, grants)
	})
}

func TestClient_GetAccount(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:      logger,
			authClients: []authtypes.QueryClient{},
		}

		account, err := client.GetAccount(ctx, "cosmos1abc123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, account)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockAuthAccountQueryClient{
			accountResp: &authtypes.QueryAccountResponse{},
		}

		client := &Client{
			logger:      logger,
			authClients: []authtypes.QueryClient{mockClient},
		}

		account, err := client.GetAccount(ctx, "cosmos1abc123")
		require.NoError(t, err)
		assert.NotNil(t, account)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			authClients: []authtypes.QueryClient{
				&mockAuthAccountQueryClient{err: assert.AnError},
				&mockAuthAccountQueryClient{err: assert.AnError},
			},
		}

		account, err := client.GetAccount(ctx, "cosmos1abc123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed on all 2 endpoints")
		assert.Nil(t, account)
	})
}

func TestClient_BroadcastTx(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{},
		}

		resp, err := client.BroadcastTx(ctx, []byte("txbytes"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, resp)
	})

	t.Run("successful broadcast", func(t *testing.T) {
		mockClient := &mockTxServiceClient{
			broadcastResp: &tx.BroadcastTxResponse{
				TxResponse: &sdktypes.TxResponse{TxHash: "0xabc", Code: 0},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{mockClient},
		}

		resp, err := client.BroadcastTx(ctx, []byte("txbytes"))
		require.NoError(t, err)
		assert.Equal(t, "0xabc", resp.TxResponse.TxHash)
	})

	t.Run("failover on first endpoint failure", func(t *testing.T) {
		failing := &mockTxServiceClient{err: assert.AnError}
		success := &mockTxServiceClient{
			broadcastResp: &tx.BroadcastTxResponse{
				TxResponse: &sdktypes.TxResponse{TxHash: "0xdef"},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{failing, success},
		}

		resp, err := client.BroadcastTx(ctx, []byte("txbytes"))
		require.NoError(t, err)
		assert.Equal(t, "0xdef", resp.TxResponse.TxHash)
	})
}

func TestClient_GetPendingTssEvents(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{},
		}

		events, err := client.GetPendingTssEvents(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, events)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			pendingTssEventsResp: &utsstypes.QueryAllPendingTssEventsResponse{
				Events: []*utsstypes.TssEvent{
					{Id: 1, ProcessId: 100, ProcessType: "TSS_PROCESS_KEYGEN"},
					{Id: 2, ProcessId: 200, ProcessType: "TSS_PROCESS_REFRESH"},
				},
			},
		}

		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{mockClient},
		}

		events, err := client.GetPendingTssEvents(ctx)
		require.NoError(t, err)
		require.Len(t, events, 2)
		assert.Equal(t, uint64(100), events[0].ProcessId)
		assert.Equal(t, uint64(200), events[1].ProcessId)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			utssClients: []utsstypes.QueryClient{
				&mockUTSSQueryClient{err: assert.AnError},
			},
		}

		events, err := client.GetPendingTssEvents(ctx)
		require.Error(t, err)
		assert.Nil(t, events)
	})
}

func TestClient_GetPendingFundMigrations(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{},
		}

		migrations, err := client.GetPendingFundMigrations(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, migrations)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			pendingFundMigrationsResp: &utsstypes.QueryPendingFundMigrationsResponse{
				Migrations: []*utsstypes.FundMigration{
					{Id: 1, OldKeyId: "key-old", Chain: "eip155:1"},
					{Id: 2, OldKeyId: "key-old", Chain: "eip155:42161"},
				},
			},
		}

		client := &Client{
			logger:      logger,
			utssClients: []utsstypes.QueryClient{mockClient},
		}

		migrations, err := client.GetPendingFundMigrations(ctx)
		require.NoError(t, err)
		require.Len(t, migrations, 2)
		assert.Equal(t, uint64(1), migrations[0].Id)
		assert.Equal(t, "eip155:42161", migrations[1].Chain)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			utssClients: []utsstypes.QueryClient{
				&mockUTSSQueryClient{err: assert.AnError},
			},
		}

		migrations, err := client.GetPendingFundMigrations(ctx)
		require.Error(t, err)
		assert.Nil(t, migrations)
	})
}

func TestClient_GetAllPendingOutbounds(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{},
		}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, entries)
		assert.Nil(t, outbounds)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockUExecutorQueryClient{
			allPendingOutboundsResp: &uexecutortypes.QueryAllPendingOutboundsResponse{
				Entries: []*uexecutortypes.PendingOutboundEntry{
					{OutboundId: "ob-1", UniversalTxId: "utx-1", CreatedAt: 100},
				},
				Outbounds: []*uexecutortypes.OutboundTx{
					{Id: "ob-1", DestinationChain: "eip155:1", Amount: "1000"},
				},
			},
		}

		client := &Client{
			logger:           logger,
			uexecutorClients: []uexecutortypes.QueryClient{mockClient},
		}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 1)
		require.Len(t, outbounds, 1)
		assert.Equal(t, "ob-1", entries[0].OutboundId)
		assert.Equal(t, "eip155:1", outbounds[0].DestinationChain)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		client := &Client{
			logger: logger,
			uexecutorClients: []uexecutortypes.QueryClient{
				&mockUExecutorQueryClient{err: assert.AnError},
			},
		}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.Error(t, err)
		assert.Nil(t, entries)
		assert.Nil(t, outbounds)
	})
}

func TestClient_GetGasPrice_NilResponse(t *testing.T) {
	logger := zerolog.Nop()
	mockClient := &mockUExecutorQueryClient{
		gasPriceResp: &uexecutortypes.QueryGasPriceResponse{
			GasPrice: nil,
		},
	}

	client := &Client{
		logger:           logger,
		uexecutorClients: []uexecutortypes.QueryClient{mockClient},
	}

	price, err := client.GetGasPrice(context.Background(), "eip155:1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GasPrice response is nil")
	assert.Nil(t, price)
}

func TestClient_GetTx(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{},
		}

		resp, err := client.GetTx(ctx, "0xabc")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, resp)
	})

	t.Run("successful get tx", func(t *testing.T) {
		mockClient := &mockTxServiceClient{
			getTxResp: &tx.GetTxResponse{
				TxResponse: &sdktypes.TxResponse{TxHash: "0xabc123", Code: 0},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{mockClient},
		}

		resp, err := client.GetTx(ctx, "0xabc123")
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "0xabc123", resp.TxResponse.TxHash)
	})

	t.Run("failover on first endpoint failure", func(t *testing.T) {
		failing := &mockTxServiceClient{getTxErr: assert.AnError}
		success := &mockTxServiceClient{
			getTxResp: &tx.GetTxResponse{
				TxResponse: &sdktypes.TxResponse{TxHash: "0xdef456"},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{failing, success},
		}

		resp, err := client.GetTx(ctx, "0xdef456")
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "0xdef456", resp.TxResponse.TxHash)
	})

	t.Run("all endpoints fail", func(t *testing.T) {
		failing1 := &mockTxServiceClient{getTxErr: assert.AnError}
		failing2 := &mockTxServiceClient{getTxErr: assert.AnError}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{failing1, failing2},
		}

		resp, err := client.GetTx(ctx, "0xabc")
		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

// Mock implementations

type mockRegistryQueryClient struct {
	uregistrytypes.QueryClient
	allChainConfigsResp *uregistrytypes.QueryAllChainConfigsResponse
	err                 error

	chainConfigPages []*uregistrytypes.QueryAllChainConfigsResponse
	chainConfigKeys  [][]byte
}

func (m *mockRegistryQueryClient) AllChainConfigs(ctx context.Context, req *uregistrytypes.QueryAllChainConfigsRequest, opts ...grpc.CallOption) (*uregistrytypes.QueryAllChainConfigsResponse, error) {
	if m.chainConfigPages != nil {
		var key []byte
		if req.Pagination != nil {
			key = req.Pagination.Key
		}
		m.chainConfigKeys = append(m.chainConfigKeys, key)
		idx := len(m.chainConfigKeys) - 1
		if idx >= len(m.chainConfigPages) {
			return nil, assert.AnError
		}
		return m.chainConfigPages[idx], nil
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.allChainConfigsResp, nil
}

func (m *mockRegistryQueryClient) ChainConfig(ctx context.Context, req *uregistrytypes.QueryChainConfigRequest, opts ...grpc.CallOption) (*uregistrytypes.QueryChainConfigResponse, error) {
	return nil, nil
}

type mockCometBFTServiceClient struct {
	cmtservice.ServiceClient
	getLatestBlockResp *cmtservice.GetLatestBlockResponse
	err                error
}

func (m *mockCometBFTServiceClient) GetLatestBlock(ctx context.Context, req *cmtservice.GetLatestBlockRequest, opts ...grpc.CallOption) (*cmtservice.GetLatestBlockResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.getLatestBlockResp, nil
}

func (m *mockCometBFTServiceClient) GetBlockByHeight(ctx context.Context, req *cmtservice.GetBlockByHeightRequest, opts ...grpc.CallOption) (*cmtservice.GetBlockByHeightResponse, error) {
	return nil, nil
}

type mockUValidatorQueryClient struct {
	uvalidatortypes.QueryClient
	allUniversalValidatorsResp *uvalidatortypes.QueryUniversalValidatorsSetResponse
	err                        error
}

func (m *mockUValidatorQueryClient) AllUniversalValidators(ctx context.Context, req *uvalidatortypes.QueryUniversalValidatorsSetRequest, opts ...grpc.CallOption) (*uvalidatortypes.QueryUniversalValidatorsSetResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.allUniversalValidatorsResp, nil
}

func (m *mockUValidatorQueryClient) UniversalValidator(ctx context.Context, req *uvalidatortypes.QueryUniversalValidatorRequest, opts ...grpc.CallOption) (*uvalidatortypes.QueryUniversalValidatorResponse, error) {
	return nil, nil
}

type mockUTSSQueryClient struct {
	utsstypes.QueryClient
	currentKeyResp            *utsstypes.QueryCurrentKeyResponse
	keyByIdResp               *utsstypes.QueryKeyByIdResponse
	pendingTssEventsResp      *utsstypes.QueryAllPendingTssEventsResponse
	pendingFundMigrationsResp *utsstypes.QueryPendingFundMigrationsResponse
	err                       error
}

func (m *mockUTSSQueryClient) CurrentKey(ctx context.Context, req *utsstypes.QueryCurrentKeyRequest, opts ...grpc.CallOption) (*utsstypes.QueryCurrentKeyResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentKeyResp, nil
}

func (m *mockUTSSQueryClient) AllPendingTssEvents(ctx context.Context, req *utsstypes.QueryAllPendingTssEventsRequest, opts ...grpc.CallOption) (*utsstypes.QueryAllPendingTssEventsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pendingTssEventsResp, nil
}

func (m *mockUTSSQueryClient) PendingFundMigrations(ctx context.Context, req *utsstypes.QueryPendingFundMigrationsRequest, opts ...grpc.CallOption) (*utsstypes.QueryPendingFundMigrationsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.pendingFundMigrationsResp, nil
}

func (m *mockUTSSQueryClient) KeyById(ctx context.Context, req *utsstypes.QueryKeyByIdRequest, opts ...grpc.CallOption) (*utsstypes.QueryKeyByIdResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.keyByIdResp, nil
}

type mockTxServiceClient struct {
	tx.ServiceClient
	broadcastResp *tx.BroadcastTxResponse
	getTxResp     *tx.GetTxResponse
	err           error
	getTxErr      error
}

func (m *mockTxServiceClient) BroadcastTx(ctx context.Context, req *tx.BroadcastTxRequest, opts ...grpc.CallOption) (*tx.BroadcastTxResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.broadcastResp, nil
}

func (m *mockTxServiceClient) GetTx(ctx context.Context, req *tx.GetTxRequest, opts ...grpc.CallOption) (*tx.GetTxResponse, error) {
	if m.getTxErr != nil {
		return nil, m.getTxErr
	}
	return m.getTxResp, nil
}

type mockUExecutorQueryClient struct {
	uexecutortypes.QueryClient
	gasPriceResp            *uexecutortypes.QueryGasPriceResponse
	allPendingOutboundsResp *uexecutortypes.QueryAllPendingOutboundsResponse
	err                     error

	// lastPendingReq records the request so tests can assert the limit sent.
	lastPendingReq *uexecutortypes.QueryAllPendingOutboundsRequest

	// pendingReqs records every page request of a walk.
	pendingReqs []*uexecutortypes.QueryAllPendingOutboundsRequest
	// pendingTotal, when set, makes the mock serve that many rows by offset.
	pendingTotal int
}

func (m *mockUExecutorQueryClient) GasPrice(ctx context.Context, req *uexecutortypes.QueryGasPriceRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryGasPriceResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.gasPriceResp, nil
}

func (m *mockUExecutorQueryClient) Params(ctx context.Context, req *uexecutortypes.QueryParamsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryParamsResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllPendingInbounds(ctx context.Context, req *uexecutortypes.QueryAllPendingInboundsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllPendingInboundsResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) GetUniversalTx(ctx context.Context, req *uexecutortypes.QueryGetUniversalTxRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryGetUniversalTxResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllUniversalTx(ctx context.Context, req *uexecutortypes.QueryAllUniversalTxRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllUniversalTxResponse, error) {
	return nil, nil
}

func (m *mockUExecutorQueryClient) AllPendingOutbounds(ctx context.Context, req *uexecutortypes.QueryAllPendingOutboundsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllPendingOutboundsResponse, error) {
	m.lastPendingReq = req
	m.pendingReqs = append(m.pendingReqs, req)
	if m.err != nil {
		return nil, m.err
	}
	if m.pendingTotal > 0 {
		offset := int(req.Pagination.GetOffset())
		end := offset + int(req.Pagination.GetLimit())
		if end > m.pendingTotal {
			end = m.pendingTotal
		}
		resp := &uexecutortypes.QueryAllPendingOutboundsResponse{
			Pagination: &query.PageResponse{Total: uint64(m.pendingTotal)},
		}
		for i := offset; i < end; i++ {
			id := fmt.Sprintf("ob-%d", i)
			resp.Entries = append(resp.Entries, &uexecutortypes.PendingOutboundEntry{OutboundId: id})
			resp.Outbounds = append(resp.Outbounds, &uexecutortypes.OutboundTx{Id: id})
		}
		return resp, nil
	}
	return m.allPendingOutboundsResp, nil
}

func (m *mockUExecutorQueryClient) AllGasPrices(ctx context.Context, req *uexecutortypes.QueryAllGasPricesRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllGasPricesResponse, error) {
	return nil, nil
}

type mockAuthzQueryClient struct {
	authz.QueryClient
	granteeGrantsResp *authz.QueryGranteeGrantsResponse
	err               error
}

func (m *mockAuthzQueryClient) GranteeGrants(ctx context.Context, req *authz.QueryGranteeGrantsRequest, opts ...grpc.CallOption) (*authz.QueryGranteeGrantsResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.granteeGrantsResp, nil
}

type mockAuthAccountQueryClient struct {
	authtypes.QueryClient
	accountResp *authtypes.QueryAccountResponse
	err         error
}

func (m *mockAuthAccountQueryClient) Account(ctx context.Context, req *authtypes.QueryAccountRequest, opts ...grpc.CallOption) (*authtypes.QueryAccountResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.accountResp, nil
}

func TestClient_GetKeyByID(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{logger: logger, utssClients: []utsstypes.QueryClient{}}

		key, err := client.GetKeyByID(context.Background(), "key-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, key)
	})

	t.Run("successful query returns key", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			keyByIdResp: &utsstypes.QueryKeyByIdResponse{
				Key: &utsstypes.TssKey{KeyId: "key-123", TssPubkey: "0xpub"},
			},
		}
		client := &Client{logger: logger, utssClients: []utsstypes.QueryClient{mockClient}}

		key, err := client.GetKeyByID(context.Background(), "key-123")
		require.NoError(t, err)
		require.NotNil(t, key)
		assert.Equal(t, "key-123", key.KeyId)
		assert.Equal(t, "0xpub", key.TssPubkey)
	})

	t.Run("unknown key id errors", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{
			keyByIdResp: &utsstypes.QueryKeyByIdResponse{Key: nil},
		}
		client := &Client{logger: logger, utssClients: []utsstypes.QueryClient{mockClient}}

		key, err := client.GetKeyByID(context.Background(), "missing")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Nil(t, key)
	})

	// A nil response with a nil error must not panic.
	t.Run("nil response errors", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{keyByIdResp: nil}
		client := &Client{logger: logger, utssClients: []utsstypes.QueryClient{mockClient}}

		key, err := client.GetKeyByID(context.Background(), "key-123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
		assert.Nil(t, key)
	})

	t.Run("query error propagates", func(t *testing.T) {
		mockClient := &mockUTSSQueryClient{err: errors.New("rpc down")}
		client := &Client{logger: logger, utssClients: []utsstypes.QueryClient{mockClient}}

		key, err := client.GetKeyByID(context.Background(), "key-123")
		require.Error(t, err)
		assert.Nil(t, key)
	})
}

// An outbound leaves the pending set only on a quorum vote, so rows that cannot
// reach one accumulate at the head of an oldest-first list. The walk is what
// stops them hiding everything newer.
func TestClient_GetAllPendingOutbounds_WalksOldestFirst(t *testing.T) {
	ctx := context.Background()

	newClient := func(total int) (*Client, *mockUExecutorQueryClient) {
		m := &mockUExecutorQueryClient{pendingTotal: total}
		return &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}, m
	}

	t.Run("oldest first, never reversed", func(t *testing.T) {
		client, m := newClient(9)
		_, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)

		p := m.pendingReqs[0].Pagination
		require.NotNil(t, p)
		assert.False(t, p.Reverse, "older outbounds must be read first")
		assert.Zero(t, p.Offset)
		assert.Equal(t, uint64(pendingOutboundPageSize), p.Limit)
	})

	// The ordinary case is a set that fits, and it must not cost extra requests.
	t.Run("a set that fits costs one request", func(t *testing.T) {
		client, m := newClient(9)
		entries, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assert.Len(t, m.pendingReqs, 1)
		assert.Len(t, entries, 9)
	})

	// A stuck prefix must not hide what is behind it.
	t.Run("walks past a full first page", func(t *testing.T) {
		client, m := newClient(pendingOutboundPageSize + 250)
		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)

		require.Len(t, m.pendingReqs, 2)
		assert.Equal(t, uint64(0), m.pendingReqs[0].Pagination.GetOffset())
		assert.Equal(t, uint64(pendingOutboundPageSize), m.pendingReqs[1].Pagination.GetOffset())

		require.Len(t, entries, pendingOutboundPageSize+250)
		require.Len(t, outbounds, pendingOutboundPageSize+250)
		assert.Equal(t, "ob-0", entries[0].OutboundId, "oldest first")
		assert.Equal(t, fmt.Sprintf("ob-%d", pendingOutboundPageSize+249), entries[len(entries)-1].OutboundId)
	})

	// The cap bounds one poll; the rest is read on the next tick.
	t.Run("stops at the row budget and says so", func(t *testing.T) {
		var logBuf bytes.Buffer
		m := &mockUExecutorQueryClient{pendingTotal: pendingOutboundMaxRows + 2*pendingOutboundPageSize}
		client := &Client{logger: zerolog.New(&logBuf), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assert.Len(t, m.pendingReqs, pendingOutboundMaxRows/pendingOutboundPageSize)
		assert.Len(t, entries, pendingOutboundMaxRows)
		assert.Contains(t, logBuf.String(), "row budget reached")
	})

	t.Run("quiet when the set fits", func(t *testing.T) {
		var logBuf bytes.Buffer
		m := &mockUExecutorQueryClient{pendingTotal: 9}
		client := &Client{logger: zerolog.New(&logBuf), uexecutorClients: []uexecutortypes.QueryClient{m}}

		_, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assert.NotContains(t, logBuf.String(), "page cap reached")
	})
}

// oversizedPageClient serves rows by offset like the mock above, but rejects any
// page larger than maxServable with ResourceExhausted, the way grpc-go does when
// a response exceeds the receive limit.
type oversizedPageClient struct {
	uexecutortypes.QueryClient
	total       int
	maxServable uint64
	reqs        []*query.PageRequest
}

func (m *oversizedPageClient) AllPendingOutbounds(ctx context.Context, req *uexecutortypes.QueryAllPendingOutboundsRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllPendingOutboundsResponse, error) {
	m.reqs = append(m.reqs, req.Pagination)
	if req.Pagination.GetLimit() > m.maxServable {
		return nil, status.Error(codes.ResourceExhausted,
			"grpc: received message larger than max")
	}
	offset := int(req.Pagination.GetOffset())
	end := offset + int(req.Pagination.GetLimit())
	if end > m.total {
		end = m.total
	}
	resp := &uexecutortypes.QueryAllPendingOutboundsResponse{
		Pagination: &query.PageResponse{Total: uint64(m.total)},
	}
	for i := offset; i < end; i++ {
		id := fmt.Sprintf("ob-%d", i)
		resp.Entries = append(resp.Entries, &uexecutortypes.PendingOutboundEntry{OutboundId: id})
		resp.Outbounds = append(resp.Outbounds, &uexecutortypes.OutboundTx{Id: id})
	}
	return resp, nil
}

// A page that will not fit must not blind the poll. The client halves until the
// response fits, which terminates because core caps an outbound payload well
// below the receive limit, so a page of one always fits.
func TestGetAllPendingOutbounds_HalvesOnResourceExhausted(t *testing.T) {
	ctx := context.Background()

	t.Run("degrades and still returns every row oldest first", func(t *testing.T) {
		m := &oversizedPageClient{total: 300, maxServable: 250}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err, "an oversized page must not fail the whole poll")
		require.Len(t, entries, 300)
		require.Len(t, outbounds, 300)

		assert.Equal(t, "ob-0", entries[0].OutboundId, "oldest first")
		assert.Equal(t, "ob-299", entries[299].OutboundId)

		// 1000 rejected, then 500 rejected, then 250 serves.
		require.GreaterOrEqual(t, len(m.reqs), 3)
		assert.Equal(t, uint64(1000), m.reqs[0].Limit)
		assert.Equal(t, uint64(500), m.reqs[1].Limit)
		assert.Equal(t, uint64(250), m.reqs[2].Limit)
	})

	t.Run("offsets follow the rows actually returned", func(t *testing.T) {
		m := &oversizedPageClient{total: 300, maxServable: 250}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		_, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)

		// The second served page must start where the first ended, not at a
		// multiple of the original page size.
		var served []*query.PageRequest
		for _, r := range m.reqs {
			if r.Limit <= m.maxServable {
				served = append(served, r)
			}
		}
		require.GreaterOrEqual(t, len(served), 2)
		assert.Equal(t, uint64(0), served[0].Offset)
		assert.Equal(t, uint64(250), served[1].Offset, "no row may be skipped or read twice")
	})

	t.Run("a page of one that still fails is a real error", func(t *testing.T) {
		m := &oversizedPageClient{total: 10, maxServable: 0}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		_, _, err := client.GetAllPendingOutbounds(ctx)
		require.Error(t, err, "nothing left to halve, so the caller must see it")
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
	})

	t.Run("an unrelated error is not retried smaller", func(t *testing.T) {
		m := &mockUExecutorQueryClient{err: status.Error(codes.Unavailable, "endpoint down")}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		_, _, err := client.GetAllPendingOutbounds(ctx)
		require.Error(t, err)
		assert.Len(t, m.pendingReqs, 1, "halving is only for a response that did not fit")
	})
}

// A page that had to shrink must cost extra requests, not rows. Bounding the
// walk by iterations instead would quietly cut a degraded poll from 5000 rows to
// five times whatever the page shrank to.
func TestGetAllPendingOutbounds_RowBudgetSurvivesDegradedPages(t *testing.T) {
	ctx := context.Background()

	full := &oversizedPageClient{total: 20000, maxServable: pendingOutboundPageSize}
	fullClient := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{full}}
	fullEntries, _, err := fullClient.GetAllPendingOutbounds(ctx)
	require.NoError(t, err)

	degraded := &oversizedPageClient{total: 20000, maxServable: 250}
	degradedClient := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{degraded}}
	degradedEntries, _, err := degradedClient.GetAllPendingOutbounds(ctx)
	require.NoError(t, err)

	assert.Len(t, fullEntries, pendingOutboundMaxRows)
	assert.Len(t, degradedEntries, pendingOutboundMaxRows,
		"a shrunken page must not shrink the poll")
	assert.Greater(t, len(degraded.reqs), len(full.reqs),
		"the cost of degrading is requests, not rows")

	// Same rows, same order, whatever the page size was.
	require.Equal(t, fullEntries[0].OutboundId, degradedEntries[0].OutboundId)
	require.Equal(t, fullEntries[len(fullEntries)-1].OutboundId, degradedEntries[len(degradedEntries)-1].OutboundId)
}

// An endpoint that rejects everything but the smallest page must not turn one
// poll into thousands of requests.
func TestGetAllPendingOutbounds_RequestCapBoundsTheDegradedPath(t *testing.T) {
	m := &oversizedPageClient{total: 20000, maxServable: 1}
	client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

	entries, _, err := client.GetAllPendingOutbounds(context.Background())
	require.NoError(t, err, "a bounded walk still returns what it read")
	assert.LessOrEqual(t, len(m.reqs), pendingOutboundMaxRequests+10)
	assert.NotEmpty(t, entries, "the poll must still make progress")
	assert.Less(t, len(entries), pendingOutboundMaxRows, "the remainder waits for the next poll")
}

// The cap is only useful if the rows under it are the right ones. Asserting the
// count alone would pass on a walk that skipped a page and re-read another, so
// this checks the whole sequence, and that entries and outbounds stay aligned.
func TestGetAllPendingOutbounds_ReadsTheCappedSetExactlyOnce(t *testing.T) {
	ctx := context.Background()

	assertContiguous := func(t *testing.T, entries []*uexecutortypes.PendingOutboundEntry, outbounds []*uexecutortypes.OutboundTx) {
		t.Helper()
		require.Len(t, entries, pendingOutboundMaxRows)
		require.Len(t, outbounds, pendingOutboundMaxRows, "an outbound per entry")

		seen := make(map[string]int, len(entries))
		for i, e := range entries {
			assert.Equal(t, fmt.Sprintf("ob-%d", i), e.OutboundId, "row %d out of order", i)
			assert.Equal(t, e.OutboundId, outbounds[i].Id, "entry and outbound diverged at %d", i)
			seen[e.OutboundId]++
		}
		require.Len(t, seen, pendingOutboundMaxRows, "a row was skipped or read twice")
		for id, n := range seen {
			require.Equal(t, 1, n, "%s appeared %d times", id, n)
		}
	}

	t.Run("full pages", func(t *testing.T) {
		m := &oversizedPageClient{total: 20000, maxServable: pendingOutboundPageSize}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assertContiguous(t, entries, outbounds)
	})

	t.Run("pages that had to shrink", func(t *testing.T) {
		m := &oversizedPageClient{total: 20000, maxServable: 250}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assertContiguous(t, entries, outbounds)
	})

	t.Run("exactly the cap available stops without a second empty request", func(t *testing.T) {
		m := &oversizedPageClient{total: pendingOutboundMaxRows, maxServable: pendingOutboundPageSize}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, outbounds, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		assertContiguous(t, entries, outbounds)
		assert.Len(t, m.reqs, pendingOutboundMaxRows/pendingOutboundPageSize,
			"hitting the cap exactly must not cost an extra round trip")
	})

	t.Run("under the cap returns everything and stops early", func(t *testing.T) {
		m := &oversizedPageClient{total: 2500, maxServable: pendingOutboundPageSize}
		client := &Client{logger: zerolog.Nop(), uexecutorClients: []uexecutortypes.QueryClient{m}}

		entries, _, err := client.GetAllPendingOutbounds(ctx)
		require.NoError(t, err)
		require.Len(t, entries, 2500)
		assert.Equal(t, "ob-0", entries[0].OutboundId)
		assert.Equal(t, "ob-2499", entries[2499].OutboundId)
	})
}
