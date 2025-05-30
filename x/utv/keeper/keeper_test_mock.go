package keeper

import (
	"context"
	"testing"

	"github.com/push-protocol/push-chain/x/utv/types"
	"github.com/stretchr/testify/require"
)

// KeeperTest is a mock implementation of Keeper for testing
type KeeperTest struct {
	t                    *testing.T
	Logger               map[string]interface{}
	getAllChainConfigsFn func(ctx context.Context) ([]types.ChainConfigData, error)
	getChainConfigFn     func(ctx context.Context, chainID string) (types.ChainConfigData, error)
	verifyTxFn           func(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error)
	verifyTxToLockerFn   func(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error)
}

// NewKeeperTest creates a new KeeperTest instance
func NewKeeperTest(t *testing.T) *KeeperTest {
	return &KeeperTest{
		t:      t,
		Logger: make(map[string]interface{}),
	}
}

// SetupGetAllChainConfigs sets up the mock for GetAllChainConfigs
func (k *KeeperTest) SetupGetAllChainConfigs(fn func(ctx context.Context) ([]types.ChainConfigData, error)) {
	k.getAllChainConfigsFn = fn
}

// SetupGetChainConfig sets up the mock for GetChainConfig
func (k *KeeperTest) SetupGetChainConfig(fn func(ctx context.Context, chainID string) (types.ChainConfigData, error)) {
	k.getChainConfigFn = fn
}

// SetupVerifyExternalTransaction sets up the mock for VerifyExternalTransaction
func (k *KeeperTest) SetupVerifyExternalTransaction(fn func(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error)) {
	k.verifyTxFn = fn
}

// GetAllChainConfigs is a mock implementation for test purposes
func (k *KeeperTest) GetAllChainConfigs(ctx context.Context) ([]types.ChainConfigData, error) {
	require.NotNil(k.t, k.getAllChainConfigsFn, "GetAllChainConfigs mock not set")
	return k.getAllChainConfigsFn(ctx)
}

// GetChainConfig is a mock implementation for test purposes
func (k *KeeperTest) GetChainConfig(ctx context.Context, chainID string) (types.ChainConfigData, error) {
	require.NotNil(k.t, k.getChainConfigFn, "GetChainConfig mock not set")
	return k.getChainConfigFn(ctx, chainID)
}

// VerifyExternalTransaction is a mock implementation for test purposes
func (k *KeeperTest) VerifyExternalTransaction(ctx context.Context, txHash string, caipAddress string) (*TransactionVerificationResult, error) {
	require.NotNil(k.t, k.verifyTxFn, "VerifyExternalTransaction mock not set")
	return k.verifyTxFn(ctx, txHash, caipAddress)
}
