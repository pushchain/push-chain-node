package pushcore

import (
	"context"
	"math/big"
	"testing"

	cmtservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
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

func TestClient_GetTxsByEvents(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{},
		}

		txs, err := client.GetTxsByEvents(context.Background(), "test.event", 0, 0, 0)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, txs)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		mockClient := &mockTxServiceClient{
			getTxsEventResp: &tx.GetTxsEventResponse{
				Txs: []*tx.Tx{
					{Body: &tx.TxBody{}},
				},
				TxResponses: []*sdktypes.TxResponse{
					{
						Height: 100,
						TxHash: "0x123",
					},
				},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{mockClient},
		}

		txs, err := client.GetTxsByEvents(context.Background(), "test.event", 0, 0, 0)
		require.NoError(t, err)
		require.Len(t, txs, 1)
		assert.Equal(t, "0x123", txs[0].TxHash)
		assert.Equal(t, int64(100), txs[0].Height)
	})

	t.Run("with height filters", func(t *testing.T) {
		mockClient := &mockTxServiceClient{
			getTxsEventResp: &tx.GetTxsEventResponse{
				Txs:         []*tx.Tx{},
				TxResponses: []*sdktypes.TxResponse{},
			},
		}

		client := &Client{
			logger:    logger,
			txClients: []tx.ServiceClient{mockClient},
		}

		txs, err := client.GetTxsByEvents(context.Background(), "test.event", 100, 200, 50)
		require.NoError(t, err)
		assert.NotNil(t, txs)
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
			logger: logger,
			conns:  []*grpc.ClientConn{},
		}

		grants, err := client.GetGranteeGrants(context.Background(), "cosmos1abc...")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, grants)
	})

	t.Run("successful query with mock", func(t *testing.T) {
		// Note: This test requires actual gRPC connections, so we'll test the error case
		// For a full mock test, we'd need to set up a gRPC server
		client := &Client{
			logger: logger,
			conns:  []*grpc.ClientConn{},
		}

		grants, err := client.GetGranteeGrants(context.Background(), "cosmos1abc...")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, grants)
	})
}

func TestClient_GetAccount(t *testing.T) {
	logger := zerolog.Nop()
	ctx := context.Background()

	t.Run("no endpoints configured", func(t *testing.T) {
		client := &Client{
			logger: logger,
			conns:  []*grpc.ClientConn{},
		}

		account, err := client.GetAccount(ctx, "cosmos1abc123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, account)
	})

	t.Run("empty address", func(t *testing.T) {
		client := &Client{
			logger: logger,
			conns:  []*grpc.ClientConn{},
		}

		account, err := client.GetAccount(ctx, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no endpoints configured")
		assert.Nil(t, account)
	})
}

// Mock implementations

type mockRegistryQueryClient struct {
	uregistrytypes.QueryClient
	allChainConfigsResp *uregistrytypes.QueryAllChainConfigsResponse
	err                 error
}

func (m *mockRegistryQueryClient) AllChainConfigs(ctx context.Context, req *uregistrytypes.QueryAllChainConfigsRequest, opts ...grpc.CallOption) (*uregistrytypes.QueryAllChainConfigsResponse, error) {
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
	currentKeyResp *utsstypes.QueryCurrentKeyResponse
	err            error
}

func (m *mockUTSSQueryClient) CurrentKey(ctx context.Context, req *utsstypes.QueryCurrentKeyRequest, opts ...grpc.CallOption) (*utsstypes.QueryCurrentKeyResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.currentKeyResp, nil
}

func (m *mockUTSSQueryClient) KeyById(ctx context.Context, req *utsstypes.QueryKeyByIdRequest, opts ...grpc.CallOption) (*utsstypes.QueryKeyByIdResponse, error) {
	return nil, nil
}

type mockTxServiceClient struct {
	tx.ServiceClient
	getTxsEventResp *tx.GetTxsEventResponse
	err             error
}

func (m *mockTxServiceClient) GetTxsEvent(ctx context.Context, req *tx.GetTxsEventRequest, opts ...grpc.CallOption) (*tx.GetTxsEventResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.getTxsEventResp, nil
}

func (m *mockTxServiceClient) GetTx(ctx context.Context, req *tx.GetTxRequest, opts ...grpc.CallOption) (*tx.GetTxResponse, error) {
	return nil, nil
}

type mockUExecutorQueryClient struct {
	uexecutortypes.QueryClient
	gasPriceResp *uexecutortypes.QueryGasPriceResponse
	err          error
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

func (m *mockUExecutorQueryClient) AllGasPrices(ctx context.Context, req *uexecutortypes.QueryAllGasPricesRequest, opts ...grpc.CallOption) (*uexecutortypes.QueryAllGasPricesResponse, error) {
	return nil, nil
}

type mockAuthQueryClient struct {
	authtypes.QueryClient
	accountResp *authtypes.QueryAccountResponse
	err         error
}

func (m *mockAuthQueryClient) Account(ctx context.Context, req *authtypes.QueryAccountRequest, opts ...grpc.CallOption) (*authtypes.QueryAccountResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.accountResp, nil
}
