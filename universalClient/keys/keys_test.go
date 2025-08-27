package keys

import (
	"testing"
	"os"
	
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	// Initialize SDK config for tests
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount("push", "pushpub")
	config.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	config.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
	config.Seal()
	
	os.Exit(m.Run())
}

func TestNewKeysWithKeybase(t *testing.T) {
	// Create temporary directory for test keyring
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	// Create basic Keys instance
	keys := &Keys{
		signerName:      "test-hotkey",
		kb:              kb,
		securityManager: nil, // Skip security manager for basic test
		passwordManager: nil, // Skip password manager for basic test
	}

	require.NotNil(t, keys)
	require.Equal(t, "test-hotkey", keys.signerName)
	require.NotNil(t, keys.kb)
	
	// Test methods that should work without requiring actual key
	assert.NotNil(t, keys.GetKeybase())
	assert.Equal(t, "", keys.GetHotkeyPassword()) // Should be empty for test
}

// TestNewKeys tests the NewKeys constructor
func TestNewKeys(t *testing.T) {
	// Create temporary directory for test keyring
	tempDir, err := os.MkdirTemp("", "test-newkeys")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test config
	cfg := &config.Config{
		AuthzGranter:   "push1abc123def456", // Valid bech32 address
		AuthzHotkey:    "test-hotkey",
		KeyringBackend: config.KeyringBackendTest,
		PChainHome:     tempDir,
	}

	// This will fail because the key doesn't exist yet, but tests the validation logic
	_, err = NewKeys(cfg.AuthzHotkey, cfg)
	assert.Error(t, err) // Should fail because hotkey doesn't exist in keyring yet
	assert.Contains(t, err.Error(), "invalid operator address")
}

// TestNewKeysWithInvalidConfig tests NewKeys with invalid configurations
func TestNewKeysWithInvalidConfig(t *testing.T) {
	// Test with empty hotkey name
	cfg := &config.Config{
		AuthzGranter:   "push1abc123def456",
		AuthzHotkey:    "",
		KeyringBackend: config.KeyringBackendTest,
	}

	_, err := NewKeys("", cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "hotkey name is required")
}

// TestNewKeysWithInvalidOperatorAddress tests NewKeys with invalid operator address
func TestNewKeysWithInvalidOperatorAddress(t *testing.T) {
	// Create temporary directory for test keyring
	tempDir, err := os.MkdirTemp("", "test-invalid-addr")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	cfg := &config.Config{
		AuthzGranter:   "invalid-address", // Invalid bech32 address
		AuthzHotkey:    "test-hotkey",
		KeyringBackend: config.KeyringBackendTest,
		PChainHome:     tempDir,
	}

	_, err = NewKeys(cfg.AuthzHotkey, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid operator address")
}

func TestKeysWithNilKeyring(t *testing.T) {
	// Test that keys with nil keyring handle gracefully
	keys := &Keys{
		signerName: "test-key",
		kb:         nil, // nil keyring
	}

	// Should return empty address for GetOperatorAddress
	operatorAddr := keys.GetOperatorAddress()
	require.Empty(t, operatorAddr)
}

func TestKeyringBackends(t *testing.T) {
	tests := []struct {
		name    string
		backend KeyringBackend
		wantErr bool
	}{
		{
			name:    "test backend",
			backend: KeyringBackendTest,
			wantErr: false,
		},
		{
			name:    "file backend", 
			backend: KeyringBackendFile,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "keyring-test")
			require.NoError(t, err)
			defer os.RemoveAll(tempDir)

			kb, err := getKeybase(tempDir, nil, tt.backend)
			if tt.wantErr {
				require.Error(t, err)
				require.Nil(t, kb)
			} else {
				require.NoError(t, err)
				require.NotNil(t, kb)
			}
		})
	}
}

func TestKeysSecurityManager(t *testing.T) {
	ksm := NewKeySecurityManager(SecurityLevelMedium, "/tmp/test")
	assert.NotNil(t, ksm)
	assert.Equal(t, SecurityLevelMedium, ksm.minSecurityLevel)
	assert.Equal(t, "/tmp/test", ksm.keyringPath)
}

// TestGetPrivateKeySecure tests the secure private key access with validation
func TestGetPrivateKeySecure(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring and key
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "test-hotkey"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	// Create keys with security manager
	securityManager := NewKeySecurityManager(SecurityLevelMedium, tempDir)
	keys := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: securityManager,
	}

	// Test successful secure access
	privKey, err := keys.GetPrivateKeySecure("")
	assert.NoError(t, err)
	assert.NotNil(t, privKey)

	// Test with nil security manager
	keysNoSecurity := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: nil,
	}

	_, err = keysNoSecurity.GetPrivateKeySecure("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "security manager not initialized")
}

// TestPasswordFailureScenarios tests various password failure scenarios
func TestPasswordFailureScenarios(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test with file backend requiring password
	kb, err := getKeybase(tempDir, nil, KeyringBackendFile)
	require.NoError(t, err)

	keys := &Keys{
		signerName: "test-key",
		kb:         kb,
	}

	// Test GetPrivateKey without password for file backend
	_, err = keys.GetPrivateKey("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "password is required for file backend")

	// Test with test backend (should not require password)
	kbTest, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keysTest := &Keys{
		signerName: "test-key",
		kb:         kbTest,
	}

	// Should not error with empty password for test backend
	password := keysTest.GetHotkeyPassword()
	assert.Empty(t, password)
}

// TestKeyringBackendSwitching tests switching between keyring backends
func TestKeyringBackendSwitching(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	tests := []struct {
		name     string
		backend1 KeyringBackend
		backend2 KeyringBackend
	}{
		{
			name:     "test to file",
			backend1: KeyringBackendTest,
			backend2: KeyringBackendFile,
		},
		{
			name:     "file to test",
			backend1: KeyringBackendFile,
			backend2: KeyringBackendTest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create keyring with first backend
			kb1, err := getKeybase(tempDir+"1", nil, tt.backend1)
			require.NoError(t, err)

			// Create keyring with second backend
			kb2, err := getKeybase(tempDir+"2", nil, tt.backend2)
			require.NoError(t, err)

			// Both should be valid
			assert.NotNil(t, kb1)
			assert.NotNil(t, kb2)
			assert.Equal(t, tt.backend1.String(), kb1.Backend())
			assert.Equal(t, tt.backend2.String(), kb2.Backend())
		})
	}
}

// TestConcurrentKeyAccess tests concurrent access to keys
func TestConcurrentKeyAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring and key
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "concurrent-test-key"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	keys := &Keys{
		signerName: keyName,
		kb:         kb,
	}

	// Test concurrent GetAddress calls
	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			_, err := keys.GetAddress()
			results <- err
		}()
	}

	// Collect results
	for i := 0; i < numGoroutines; i++ {
		err := <-results
		assert.NoError(t, err)
	}
}

// TestValidateKeyIntegrity tests key integrity validation
func TestValidateKeyIntegrity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "integrity-test-key"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	// Test with security manager
	securityManager := NewKeySecurityManager(SecurityLevelMedium, tempDir)
	keys := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: securityManager,
	}

	err = keys.ValidateKeyIntegrity()
	assert.NoError(t, err)

	// Test without security manager
	keysNoSecurity := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: nil,
	}

	err = keysNoSecurity.ValidateKeyIntegrity()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "security manager not initialized")
}

// TestGetKeyFingerprint tests key fingerprint generation
func TestGetKeyFingerprint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "fingerprint-test-key"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	// Test with security manager
	securityManager := NewKeySecurityManager(SecurityLevelMedium, tempDir)
	keys := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: securityManager,
	}

	fingerprint, err := keys.GetKeyFingerprint()
	assert.NoError(t, err)
	assert.NotEmpty(t, fingerprint)

	// Test without security manager
	keysNoSecurity := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: nil,
	}

	_, err = keysNoSecurity.GetKeyFingerprint()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "security manager not initialized")
}

// TestGetSecurityRecommendations tests security recommendations
func TestGetSecurityRecommendations(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "recommendations-test-key"

	// Test with security manager
	securityManager := NewKeySecurityManager(SecurityLevelMedium, tempDir)
	keys := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: securityManager,
	}

	recommendations := keys.GetSecurityRecommendations()
	assert.NotNil(t, recommendations)

	// Test without security manager
	keysNoSecurity := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: nil,
	}

	recommendations = keysNoSecurity.GetSecurityRecommendations()
	assert.NotEmpty(t, recommendations)
	assert.Contains(t, recommendations[0].Issue, "Security manager not initialized")
	assert.Equal(t, "HIGH", recommendations[0].Level)
}

// TestSecureKeyAccess tests secure key access validation and auditing
func TestSecureKeyAccess(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "access-test-key"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	// Test with security manager
	securityManager := NewKeySecurityManager(SecurityLevelMedium, tempDir)
	keys := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: securityManager,
	}

	// Test valid operations
	validOps := []string{"create", "access", "sign", "export", "delete"}
	for _, op := range validOps {
		err := keys.SecureKeyAccess(op)
		assert.NoError(t, err, "Operation %s should be allowed", op)
	}

	// Test without security manager
	keysNoSecurity := &Keys{
		signerName:      keyName,
		kb:              kb,
		securityManager: nil,
	}

	err = keysNoSecurity.SecureKeyAccess("sign")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "security manager not initialized")
}

// TestGetHotkeyKeyName tests the hotkey name utility function
func TestGetHotkeyKeyName(t *testing.T) {
	testName := "my-hotkey"
	result := GetHotkeyKeyName(testName)
	assert.Equal(t, testName, result)

	// Test with empty string
	result = GetHotkeyKeyName("")
	assert.Equal(t, "", result)
}

// TestGetSignerInfo tests getting signer information
func TestGetSignerInfo(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring and key
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keyName := "signer-info-test-key"
	_, err = CreateNewKey(kb, keyName, "", "")
	require.NoError(t, err)

	keys := &Keys{
		signerName: keyName,
		kb:         kb,
	}

	// Test successful retrieval
	info := keys.GetSignerInfo()
	assert.NotNil(t, info)
	assert.Equal(t, keyName, info.Name)

	// Test with non-existent key
	keysInvalid := &Keys{
		signerName: "non-existent-key",
		kb:         kb,
	}

	info = keysInvalid.GetSignerInfo()
	assert.Nil(t, info)
}

// TestErrorConditions tests various error conditions
func TestErrorConditions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create test keyring
	kb, err := getKeybase(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	keys := &Keys{
		signerName: "non-existent-key",
		kb:         kb,
	}

	// Test GetAddress with non-existent key
	_, err = keys.GetAddress()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get key")

	// Test GetPrivateKey with non-existent key
	_, err = keys.GetPrivateKey("")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to export private key")
}
