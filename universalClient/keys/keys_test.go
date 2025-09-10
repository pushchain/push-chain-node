package keys

import (
	"os"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test keyring
	kb, err := CreateKeyring(tempDir, nil, KeyringBackendTest)
	require.NoError(t, err)

	// Create basic Keys instance
	keys := &Keys{
		signerName: "test-hotkey",
		kb:         kb,
	}

	require.NotNil(t, keys)
	require.Equal(t, "test-hotkey", keys.signerName)
	require.NotNil(t, keys.kb)

	// Test methods that should work without requiring actual key
	assert.NotNil(t, keys.kb)
	assert.Equal(t, "", keys.GetHotkeyPassword()) // Should be empty for test
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
			defer func() { _ = os.RemoveAll(tempDir) }()

			kb, err := CreateKeyring(tempDir, nil, tt.backend)
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

// TestPasswordFailureScenarios tests various password failure scenarios
func TestPasswordFailureScenarios(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Test with file backend requiring password
	kb, err := CreateKeyring(tempDir, nil, KeyringBackendFile)
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
	kbTest, err := CreateKeyring(tempDir, nil, KeyringBackendTest)
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
	defer func() { _ = os.RemoveAll(tempDir) }()

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
			kb1, err := CreateKeyring(tempDir+"1", nil, tt.backend1)
			require.NoError(t, err)

			// Create keyring with second backend
			kb2, err := CreateKeyring(tempDir+"2", nil, tt.backend2)
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
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test keyring and key
	kb, err := CreateKeyring(tempDir, nil, KeyringBackendTest)
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


// TestErrorConditions tests various error conditions
func TestErrorConditions(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "test-keyring")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Create test keyring
	kb, err := CreateKeyring(tempDir, nil, KeyringBackendTest)
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
