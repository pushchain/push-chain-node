package pushsigner

import (
	"context"
	"fmt"
	"os"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	cosmosauthz "github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner/keys"
)

func TestMain(m *testing.M) {
	sdkConfig := sdk.GetConfig()
	func() {
		defer func() {
			_ = recover()
		}()
		sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
		sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
		sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
		sdkConfig.Seal()
	}()

	os.Exit(m.Run())
}

// --- mock chainClient ---

type mockChainClient struct {
	broadcastTxFn     func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error)
	getAccountFn      func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error)
	getGranteeGrantFn func(ctx context.Context, addr string) (*cosmosauthz.QueryGranteeGrantsResponse, error)
}

func (m *mockChainClient) BroadcastTx(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
	if m.broadcastTxFn != nil {
		return m.broadcastTxFn(ctx, txBytes)
	}
	return nil, fmt.Errorf("BroadcastTx not mocked")
}

func (m *mockChainClient) GetAccount(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
	if m.getAccountFn != nil {
		return m.getAccountFn(ctx, address)
	}
	return nil, fmt.Errorf("GetAccount not mocked")
}

func (m *mockChainClient) GetGranteeGrants(ctx context.Context, addr string) (*cosmosauthz.QueryGranteeGrantsResponse, error) {
	if m.getGranteeGrantFn != nil {
		return m.getGranteeGrantFn(ctx, addr)
	}
	return nil, fmt.Errorf("GetGranteeGrants not mocked")
}

// --- helpers ---

func createMockPushCoreClient() *pushcore.Client {
	return &pushcore.Client{}
}

// createTestSigner creates a Signer with a real keyring and mock chainClient for testing.
func createTestSigner(t *testing.T, mock *mockChainClient) *Signer {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "signer-test")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	kr, err := keys.CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	record, _, err := keys.CreateNewKey(kr, "test-key", "", "")
	require.NoError(t, err)

	k := keys.NewKeys(kr, record.Name)
	clientCtx := createClientContext(kr, "test-chain")

	return &Signer{
		keys:      k,
		clientCtx: clientCtx,
		pushCore:  mock,
		granter:   "push1granter",
		log:       zerolog.New(zerolog.NewTestWriter(t)),
	}
}

// makeAccountResponse creates a mock QueryAccountResponse with the given sequence and account number.
func makeAccountResponse(t *testing.T, address sdk.AccAddress, seq, accNum uint64) *authtypes.QueryAccountResponse {
	t.Helper()

	baseAccount := &authtypes.BaseAccount{
		Address:       address.String(),
		AccountNumber: accNum,
		Sequence:      seq,
	}

	anyAccount, err := codectypes.NewAnyWithValue(baseAccount)
	require.NoError(t, err)

	return &authtypes.QueryAccountResponse{
		Account: anyAccount,
	}
}

// --- New() tests ---

func TestNew(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("validation failure - no keys in keyring", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-signer")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		mockCore := createMockPushCoreClient()

		signer, err := New(context.Background(), logger, config.KeyringBackendTest, "", "", mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "PushSigner validation failed")
	})

	t.Run("validation failure - keyring creation fails", func(t *testing.T) {
		mockCore := createMockPushCoreClient()

		signer, err := New(context.Background(), logger, config.KeyringBackendFile, "", "", mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
		assert.Contains(t, err.Error(), "keyring_password is required for file backend")
	})

	t.Run("validation failure - no grants", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "test-signer")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		kr, err := keys.CreateKeyring(tempDir, nil, config.KeyringBackendTest)
		require.NoError(t, err)

		_, _, err = keys.CreateNewKey(kr, "test-key", "", "")
		require.NoError(t, err)

		mockCore := createMockPushCoreClient()

		signer, err := New(context.Background(), logger, config.KeyringBackendTest, "", tempDir, mockCore, "test-chain", "cosmos1granter")
		require.Error(t, err)
		assert.Nil(t, signer)
	})
}

// --- Keys tests ---

func TestSigner_GetKeyring(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-signer")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	kr, err := keys.CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	record, _, err := keys.CreateNewKey(kr, "test-key", "", "")
	require.NoError(t, err)

	k := keys.NewKeys(kr, record.Name)

	t.Run("valid key", func(t *testing.T) {
		keyring, err := k.GetKeyring()
		require.NoError(t, err)
		assert.NotNil(t, keyring)
	})

	t.Run("invalid key", func(t *testing.T) {
		invalidK := keys.NewKeys(kr, "non-existent-key")
		keyring, err := invalidK.GetKeyring()
		require.Error(t, err)
		assert.Nil(t, keyring)
		assert.Contains(t, err.Error(), "not found in keyring")
	})
}

// --- AuthZ wrapping tests ---

func TestWrapWithAuthZ(t *testing.T) {
	mock := &mockChainClient{}
	signer := createTestSigner(t, mock)

	t.Run("empty messages returns error", func(t *testing.T) {
		msgs, err := signer.wrapWithAuthZ(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no messages to wrap")
		assert.Nil(t, msgs)
	})

	t.Run("wraps messages with MsgExec", func(t *testing.T) {
		innerMsg := &cosmosauthz.MsgExec{}
		msgs, err := signer.wrapWithAuthZ([]sdk.Msg{innerMsg})
		require.NoError(t, err)
		require.Len(t, msgs, 1)

		exec, ok := msgs[0].(*cosmosauthz.MsgExec)
		require.True(t, ok, "expected MsgExec wrapper")
		assert.Len(t, exec.Msgs, 1)
	})
}

// --- TxBuilder tests ---

func TestCreateTxBuilder(t *testing.T) {
	mock := &mockChainClient{}
	signer := createTestSigner(t, mock)

	t.Run("creates tx builder with params", func(t *testing.T) {
		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		txBuilder, err := signer.createTxBuilder(
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test memo",
			200000,
			fee,
		)
		require.NoError(t, err)
		require.NotNil(t, txBuilder)

		builtTx := txBuilder.GetTx()
		assert.Equal(t, "test memo", builtTx.GetMemo())
		assert.Equal(t, uint64(200000), builtTx.GetGas())
		assert.Equal(t, fee, builtTx.GetFee())
	})
}

// --- Account info tests ---

func TestGetAccountInfo(t *testing.T) {
	t.Run("returns account from chain", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 42, 7), nil
			},
		}

		signer := createTestSigner(t, mock)

		account, err := signer.getAccountInfo(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(42), account.GetSequence())
		assert.Equal(t, uint64(7), account.GetAccountNumber())
	})

	t.Run("returns error on chain failure", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				return nil, fmt.Errorf("node unavailable")
			},
		}

		signer := createTestSigner(t, mock)

		account, err := signer.getAccountInfo(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "node unavailable")
		assert.Nil(t, account)
	})
}

// --- Sign and broadcast tests ---

func TestSignAndBroadcast_SequenceManagement(t *testing.T) {
	t.Run("successful broadcast increments sequence", func(t *testing.T) {
		broadcastCalls := 0
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 5, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				broadcastCalls++
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "ABC123"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)
		assert.Equal(t, uint64(0), signer.lastSequence)

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		resp, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Equal(t, "ABC123", resp.TxHash)
		assert.Equal(t, uint64(6), signer.lastSequence)
		assert.Equal(t, 1, broadcastCalls)
	})

	t.Run("network error resets sequence to 0", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 10, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				return nil, fmt.Errorf("connection refused")
			},
		}

		signer := createTestSigner(t, mock)
		signer.lastSequence = 10

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		_, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "connection refused")
		assert.Equal(t, uint64(0), signer.lastSequence)
	})

	t.Run("sequence mismatch error retries with refresh", func(t *testing.T) {
		attempt := 0
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				if attempt > 0 {
					return makeAccountResponse(t, addr, 7, 1), nil
				}
				return makeAccountResponse(t, addr, 5, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				attempt++
				if attempt == 1 {
					return nil, fmt.Errorf("account sequence mismatch: expected 7, got 5")
				}
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "RETRY_OK"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		resp, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		assert.Equal(t, "RETRY_OK", resp.TxHash)
		assert.Equal(t, 2, attempt)
		assert.Equal(t, uint64(8), signer.lastSequence)
	})

	t.Run("on-chain error increments sequence", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 3, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 5, TxHash: "FAILED_TX", RawLog: "insufficient funds"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		resp, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "insufficient funds")
		assert.NotNil(t, resp)
		assert.Equal(t, uint64(4), signer.lastSequence)
	})

	t.Run("on-chain sequence mismatch retries", func(t *testing.T) {
		attempt := 0
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				if attempt > 0 {
					return makeAccountResponse(t, addr, 9, 1), nil
				}
				return makeAccountResponse(t, addr, 5, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				attempt++
				if attempt == 1 {
					return &sdktx.BroadcastTxResponse{
						TxResponse: &sdk.TxResponse{Code: 32, RawLog: "account sequence mismatch: expected 9, got 5"},
					}, nil
				}
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "OK_AFTER_RETRY"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		resp, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		assert.Equal(t, "OK_AFTER_RETRY", resp.TxHash)
		assert.Equal(t, 2, attempt)
	})
}

// --- Sequence reconciliation tests ---

func TestSequenceReconciliation(t *testing.T) {
	t.Run("local=0 adopts chain sequence", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 15, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "OK"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)
		signer.lastSequence = 0

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		_, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		assert.Equal(t, uint64(16), signer.lastSequence)
	})

	t.Run("local behind chain adopts chain", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 20, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "OK"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)
		signer.lastSequence = 10

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		_, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		assert.Equal(t, uint64(21), signer.lastSequence)
	})

	t.Run("local ahead of chain keeps local", func(t *testing.T) {
		mock := &mockChainClient{
			getAccountFn: func(ctx context.Context, address string) (*authtypes.QueryAccountResponse, error) {
				addr, _ := sdk.AccAddressFromBech32(address)
				return makeAccountResponse(t, addr, 5, 1), nil
			},
			broadcastTxFn: func(ctx context.Context, txBytes []byte) (*sdktx.BroadcastTxResponse, error) {
				return &sdktx.BroadcastTxResponse{
					TxResponse: &sdk.TxResponse{Code: 0, TxHash: "OK"},
				}, nil
			},
		}

		signer := createTestSigner(t, mock)
		signer.lastSequence = 8

		fee := sdk.NewCoins(sdk.NewInt64Coin("upc", 1000))
		_, err := signer.signAndBroadcastAuthZTx(
			context.Background(),
			[]sdk.Msg{&cosmosauthz.MsgExec{}},
			"test", 200000, fee,
		)

		require.NoError(t, err)
		assert.Equal(t, uint64(9), signer.lastSequence)
	})
}
