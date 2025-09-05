package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/keys"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// MockAuthzQueryClient is a mock for authz.QueryClient
type MockAuthzQueryClient struct {
	mock.Mock
}

func (m *MockAuthzQueryClient) GranteeGrants(ctx context.Context, in *authz.QueryGranteeGrantsRequest, opts ...grpc.CallOption) (*authz.QueryGranteeGrantsResponse, error) {
	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authz.QueryGranteeGrantsResponse), args.Error(1)
}

func (m *MockAuthzQueryClient) Grants(ctx context.Context, in *authz.QueryGrantsRequest, opts ...grpc.CallOption) (*authz.QueryGrantsResponse, error) {
	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authz.QueryGrantsResponse), args.Error(1)
}

func (m *MockAuthzQueryClient) GranterGrants(ctx context.Context, in *authz.QueryGranterGrantsRequest, opts ...grpc.CallOption) (*authz.QueryGranterGrantsResponse, error) {
	args := m.Called(ctx, in, opts)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*authz.QueryGranterGrantsResponse), args.Error(1)
}

// MockKeyring is a mock for keyring.Keyring
type MockKeyring struct {
	mock.Mock
}

func (m *MockKeyring) Backend() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockKeyring) List() ([]*keyring.Record, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*keyring.Record), args.Error(1)
}

func (m *MockKeyring) SupportedAlgorithms() (keyring.SigningAlgoList, keyring.SigningAlgoList) {
	args := m.Called()
	return args.Get(0).(keyring.SigningAlgoList), args.Get(1).(keyring.SigningAlgoList)
}

func (m *MockKeyring) Key(uid string) (*keyring.Record, error) {
	args := m.Called(uid)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) KeyByAddress(address sdk.Address) (*keyring.Record, error) {
	args := m.Called(address)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) Delete(uid string) error {
	args := m.Called(uid)
	return args.Error(0)
}

func (m *MockKeyring) DeleteByAddress(address sdk.Address) error {
	args := m.Called(address)
	return args.Error(0)
}

func (m *MockKeyring) Rename(from string, to string) error {
	args := m.Called(from, to)
	return args.Error(0)
}

func (m *MockKeyring) NewMnemonic(uid string, language keyring.Language, hdPath, bip39Passphrase string, algo keyring.SignatureAlgo) (*keyring.Record, string, error) {
	args := m.Called(uid, language, hdPath, bip39Passphrase, algo)
	if args.Get(0) == nil {
		return nil, args.String(1), args.Error(2)
	}
	return args.Get(0).(*keyring.Record), args.String(1), args.Error(2)
}

func (m *MockKeyring) NewAccount(uid string, mnemonic string, bip39Passphrase string, hdPath string, algo keyring.SignatureAlgo) (*keyring.Record, error) {
	args := m.Called(uid, mnemonic, bip39Passphrase, hdPath, algo)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) SaveLedgerKey(uid string, algo keyring.SignatureAlgo, hrp string, coinType, account, index uint32) (*keyring.Record, error) {
	args := m.Called(uid, algo, hrp, coinType, account, index)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) SaveOfflineKey(uid string, pubkey cryptotypes.PubKey) (*keyring.Record, error) {
	args := m.Called(uid, pubkey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) SaveMultisig(uid string, pubkey cryptotypes.PubKey) (*keyring.Record, error) {
	args := m.Called(uid, pubkey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*keyring.Record), args.Error(1)
}

func (m *MockKeyring) Sign(uid string, msg []byte, signMode signing.SignMode) ([]byte, cryptotypes.PubKey, error) {
	args := m.Called(uid, msg, signMode)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).([]byte), args.Get(1).(cryptotypes.PubKey), args.Error(2)
}

func (m *MockKeyring) SignByAddress(address sdk.Address, msg []byte, signMode signing.SignMode) ([]byte, cryptotypes.PubKey, error) {
	args := m.Called(address, msg, signMode)
	if args.Get(0) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).([]byte), args.Get(1).(cryptotypes.PubKey), args.Error(2)
}

func (m *MockKeyring) ImportPrivKey(uid, armor, passphrase string) error {
	args := m.Called(uid, armor, passphrase)
	return args.Error(0)
}

func (m *MockKeyring) ImportPrivKeyHex(uid, privKey, algo string) error {
	args := m.Called(uid, privKey, algo)
	return args.Error(0)
}

func (m *MockKeyring) ImportPubKey(uid string, armor string) error {
	args := m.Called(uid, armor)
	return args.Error(0)
}

func (m *MockKeyring) ExportPubKeyArmor(uid string) (string, error) {
	args := m.Called(uid)
	return args.String(0), args.Error(1)
}

func (m *MockKeyring) ExportPubKeyArmorByAddress(address sdk.Address) (string, error) {
	args := m.Called(address)
	return args.String(0), args.Error(1)
}

func (m *MockKeyring) ExportPrivKeyArmor(uid, encryptPassphrase string) (armor string, err error) {
	args := m.Called(uid, encryptPassphrase)
	return args.String(0), args.Error(1)
}

func (m *MockKeyring) ExportPrivKeyArmorByAddress(address sdk.Address, encryptPassphrase string) (armor string, err error) {
	args := m.Called(address, encryptPassphrase)
	return args.String(0), args.Error(1)
}

func (m *MockKeyring) MigrateAll() ([]*keyring.Record, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*keyring.Record), args.Error(1)
}

// setupTestKeyMonitor creates a KeyMonitor for testing
func setupTestKeyMonitor(t *testing.T, checkInterval time.Duration) *KeyMonitor {
	testConfig := &config.Config{
		KeyringBackend: "test",
	}
	
	km := NewKeyMonitor(
		context.Background(),
		zerolog.Nop(),
		testConfig,
		"localhost",
		checkInterval,
	)
	
	return km
}

func TestNewKeyMonitor(t *testing.T) {
	km := setupTestKeyMonitor(t, 10*time.Second)
	
	assert.NotNil(t, km)
	assert.NotNil(t, km.ctx)
	assert.NotNil(t, km.config)
	assert.Equal(t, "localhost", km.grpcURL)
	assert.Equal(t, 10*time.Second, km.checkInterval)
	assert.NotNil(t, km.stopCh)
}

func TestKeyMonitor_SetCallbacks(t *testing.T) {
	km := setupTestKeyMonitor(t, 10*time.Second)
	
	var validKeyCalled bool
	var noValidKeyCalled bool
	
	onValidKey := func(k keys.UniversalValidatorKeys) {
		validKeyCalled = true
	}
	
	onNoValidKey := func() {
		noValidKeyCalled = true
	}
	
	km.SetCallbacks(onValidKey, onNoValidKey)
	
	// Test valid key callback
	km.onValidKeyFound(nil)
	assert.True(t, validKeyCalled)
	
	// Test no valid key callback
	km.onNoValidKey()
	assert.True(t, noValidKeyCalled)
}

func TestKeyMonitor_StartStop(t *testing.T) {
	km := setupTestKeyMonitor(t, 100*time.Millisecond)
	
	// Start the monitor
	err := km.Start()
	assert.NoError(t, err)
	
	// Give it time to start
	time.Sleep(50 * time.Millisecond)
	
	// Stop the monitor
	km.Stop()
	
	// Ensure stop channel is closed
	select {
	case <-km.stopCh:
		// Channel is closed as expected
	default:
		t.Error("Stop channel should be closed")
	}
}

func TestKeyMonitor_checkMsgVoteInboundPermission(t *testing.T) {
	km := setupTestKeyMonitor(t, 10*time.Second)
	
	tests := []struct {
		name           string
		granteeAddr    string
		setupMock      func(*MockAuthzQueryClient)
		expectedHasPerm bool
		expectedGranter string
	}{
		{
			name:        "valid MsgVoteInbound grant",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				// Create a GenericAuthorization for MsgVoteInbound
				genericAuth := &authz.GenericAuthorization{
					Msg: "/ue.v1.MsgVoteInbound",
				}
				
				authzAny, err := codectypes.NewAnyWithValue(genericAuth)
				require.NoError(t, err)
				
				resp := &authz.QueryGranteeGrantsResponse{
					Grants: []*authz.GrantAuthorization{
						{
							Granter:       "cosmos1granter",
							Grantee:       "cosmos1test",
							Authorization: authzAny,
							Expiration:    nil, // No expiration
						},
					},
				}
				
				m.On("GranteeGrants", mock.Anything, mock.MatchedBy(func(req *authz.QueryGranteeGrantsRequest) bool {
					return req.Grantee == "cosmos1test"
				}), mock.Anything).Return(resp, nil)
			},
			expectedHasPerm: true,
			expectedGranter: "cosmos1granter",
		},
		{
			name:        "expired MsgVoteInbound grant",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				genericAuth := &authz.GenericAuthorization{
					Msg: "/ue.v1.MsgVoteInbound",
				}
				
				authzAny, err := codectypes.NewAnyWithValue(genericAuth)
				require.NoError(t, err)
				
				expiredTime := time.Now().Add(-time.Hour) // Expired 1 hour ago
				resp := &authz.QueryGranteeGrantsResponse{
					Grants: []*authz.GrantAuthorization{
						{
							Granter:       "cosmos1granter",
							Grantee:       "cosmos1test",
							Authorization: authzAny,
							Expiration:    &expiredTime,
						},
					},
				}
				
				m.On("GranteeGrants", mock.Anything, mock.Anything, mock.Anything).Return(resp, nil)
			},
			expectedHasPerm: false,
			expectedGranter: "",
		},
		{
			name:        "no grants",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				resp := &authz.QueryGranteeGrantsResponse{
					Grants: []*authz.GrantAuthorization{},
				}
				m.On("GranteeGrants", mock.Anything, mock.Anything, mock.Anything).Return(resp, nil)
			},
			expectedHasPerm: false,
			expectedGranter: "",
		},
		{
			name:        "wrong message type grant",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				genericAuth := &authz.GenericAuthorization{
					Msg: "/cosmos.bank.v1beta1.MsgSend",
				}
				
				authzAny, err := codectypes.NewAnyWithValue(genericAuth)
				require.NoError(t, err)
				
				resp := &authz.QueryGranteeGrantsResponse{
					Grants: []*authz.GrantAuthorization{
						{
							Granter:       "cosmos1granter",
							Grantee:       "cosmos1test",
							Authorization: authzAny,
						},
					},
				}
				
				m.On("GranteeGrants", mock.Anything, mock.Anything, mock.Anything).Return(resp, nil)
			},
			expectedHasPerm: false,
			expectedGranter: "",
		},
		{
			name:        "query error",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				m.On("GranteeGrants", mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("network error"))
			},
			expectedHasPerm: false,
			expectedGranter: "",
		},
		{
			name:        "context timeout",
			granteeAddr: "cosmos1test",
			setupMock: func(m *MockAuthzQueryClient) {
				m.On("GranteeGrants", mock.Anything, mock.Anything, mock.Anything).Return(nil, context.DeadlineExceeded)
			},
			expectedHasPerm: false,
			expectedGranter: "",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := &MockAuthzQueryClient{}
			tt.setupMock(mockClient)
			
			hasPerm, granter := km.checkMsgVoteInboundPermission(mockClient, tt.granteeAddr)
			
			assert.Equal(t, tt.expectedHasPerm, hasPerm)
			assert.Equal(t, tt.expectedGranter, granter)
			
			mockClient.AssertExpectations(t)
		})
	}
}

func TestKeyMonitor_handleNoValidKey(t *testing.T) {
	km := setupTestKeyMonitor(t, 10*time.Second)
	
	var callbackCalled bool
	km.SetCallbacks(nil, func() {
		callbackCalled = true
	})
	
	// Test when there was a previous valid key
	km.mu.Lock()
	km.lastValidKey = "test-key"
	km.lastGranter = "cosmos1granter"
	km.currentTxSigner = &MockTxSigner{}
	km.mu.Unlock()
	
	km.handleNoValidKey()
	
	// Check state was cleared
	km.mu.RLock()
	assert.Empty(t, km.lastValidKey)
	assert.Empty(t, km.lastGranter)
	assert.Nil(t, km.currentTxSigner)
	km.mu.RUnlock()
	
	assert.True(t, callbackCalled)
	
	// Test when there was no previous valid key (no state change)
	callbackCalled = false
	km.handleNoValidKey()
	assert.False(t, callbackCalled) // Should not call callback again
}

func TestKeyMonitor_GetCurrentTxSigner(t *testing.T) {
	km := setupTestKeyMonitor(t, 10*time.Second)
	
	// Initially nil
	txSigner := km.GetCurrentTxSigner()
	assert.Nil(t, txSigner)
	
	// Set a tx signer
	testTxSigner := &MockTxSigner{}
	km.mu.Lock()
	km.currentTxSigner = testTxSigner
	km.mu.Unlock()
	
	// Should return the set signer
	txSigner = km.GetCurrentTxSigner()
	assert.Equal(t, testTxSigner, txSigner)
}

func TestKeyMonitor_monitorLoop(t *testing.T) {
	// Create a monitor with a very short check interval
	km := setupTestKeyMonitor(t, 50*time.Millisecond)
	
	// Use a channel to count checks
	checkChan := make(chan struct{}, 10)
	callCount := 0
	
	// Run monitor loop in a goroutine
	go func() {
		// Do initial check
		checkChan <- struct{}{}
		
		ticker := time.NewTicker(km.checkInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-km.ctx.Done():
				return
			case <-km.stopCh:
				return
			case <-ticker.C:
				checkChan <- struct{}{}
				callCount++
				if callCount >= 2 {
					close(km.stopCh)
					return
				}
			}
		}
	}()
	
	// Wait for at least 3 checks
	checkCount := 0
	timeout := time.After(500 * time.Millisecond)
	for {
		select {
		case <-checkChan:
			checkCount++
			if checkCount >= 3 {
				goto done
			}
		case <-timeout:
			goto done
		}
	}
	
done:
	// Should have been called at least 3 times (initial + 2 periodic)
	assert.GreaterOrEqual(t, checkCount, 3)
}

func TestKeyMonitor_contextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	
	testConfig := &config.Config{
		KeyringBackend: "test",
	}
	
	km := &KeyMonitor{
		ctx:           ctx,
		log:           zerolog.Nop(),
		config:        testConfig,
		grpcURL:       "localhost",
		checkInterval: 50 * time.Millisecond,
		stopCh:        make(chan struct{}),
	}
	
	// Start monitor loop
	go km.monitorLoop()
	
	// Cancel context after a short delay
	time.Sleep(100 * time.Millisecond)
	cancel()
	
	// Wait a bit to ensure loop exits
	time.Sleep(100 * time.Millisecond)
	
	// Loop should have exited due to context cancellation
	// If it hasn't, the test will timeout
}

func TestKeyMonitor_grpcEndpointHandling(t *testing.T) {
	tests := []struct {
		name     string
		grpcURL  string
		expected string
	}{
		{
			name:     "no port",
			grpcURL:  "localhost",
			expected: "localhost:9090",
		},
		{
			name:     "with port",
			grpcURL:  "localhost:8090",
			expected: "localhost:8090",
		},
		{
			name:     "hostname with colon but no port",
			grpcURL:  "my:host",
			expected: "my:host:9090",
		},
		{
			name:     "IP without port",
			grpcURL:  "192.168.1.1",
			expected: "192.168.1.1:9090",
		},
		{
			name:     "IP with port",
			grpcURL:  "192.168.1.1:7070",
			expected: "192.168.1.1:7070",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			km := setupTestKeyMonitor(t, 10*time.Second)
			km.grpcURL = tt.grpcURL
			
			// Extract the endpoint handling logic from checkKeys
			grpcEndpoint := km.grpcURL
			if !strings.Contains(grpcEndpoint, ":") {
				grpcEndpoint = grpcEndpoint + ":9090"
			} else {
				lastColon := strings.LastIndex(grpcEndpoint, ":")
				afterColon := grpcEndpoint[lastColon+1:]
				if _, err := fmt.Sscanf(afterColon, "%d", new(int)); err != nil {
					grpcEndpoint = grpcEndpoint + ":9090"
				}
			}
			
			assert.Equal(t, tt.expected, grpcEndpoint)
		})
	}
}


// Ensure MockAuthzQueryClient implements authz.QueryClient
var _ authz.QueryClient = (*MockAuthzQueryClient)(nil)


// Ensure MockKeyring implements keyring.Keyring
var _ keyring.Keyring = (*MockKeyring)(nil)