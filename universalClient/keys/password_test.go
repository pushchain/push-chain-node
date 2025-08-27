package keys

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidatePasswordStrength(t *testing.T) {
	pm := NewPasswordManager(false, "")

	tests := []struct {
		name     string
		password string
		wantErr  bool
		errMsg   string
	}{
		{
			name:     "strong password",
			password: "MyStr0ng!Pass",
			wantErr:  false,
		},
		{
			name:     "password with all requirements",
			password: "Test123!@#",
			wantErr:  false,
		},
		{
			name:     "too short",
			password: "Test1!",
			wantErr:  true,
			errMsg:   "at least 8 characters",
		},
		{
			name:     "missing uppercase",
			password: "test123!@#",
			wantErr:  true,
			errMsg:   "uppercase letter",
		},
		{
			name:     "missing lowercase",
			password: "TEST123!@#",
			wantErr:  true,
			errMsg:   "lowercase letter",
		},
		{
			name:     "missing digit",
			password: "TestAbc!@#",
			wantErr:  true,
			errMsg:   "digit",
		},
		{
			name:     "missing special character",
			password: "TestAbc123",
			wantErr:  true,
			errMsg:   "special character",
		},
		{
			name:     "empty password",
			password: "",
			wantErr:  true,
			errMsg:   "at least 8 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := pm.ValidatePasswordStrength(tt.password)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSecurePasswordInput(t *testing.T) {
	tests := []struct {
		name      string
		backend   KeyringBackend
		operation string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "test backend create",
			backend:   KeyringBackendTest,
			operation: "create",
			wantErr:   false,
		},
		{
			name:      "test backend access",
			backend:   KeyringBackendTest,
			operation: "access",
			wantErr:   false,
		},
		{
			name:      "invalid operation",
			backend:   KeyringBackendFile,
			operation: "invalid",
			wantErr:   true,
			errMsg:    "unknown password operation",
		},
		{
			name:      "invalid backend",
			backend:   "invalid",
			operation: "create",
			wantErr:   true,
			errMsg:    "unsupported keyring backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// For file backend operations, we expect an error in non-interactive mode
			// For test backend, we expect no error and empty password
			password, err := SecurePasswordInput(tt.backend, tt.operation)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else if tt.backend == KeyringBackendTest {
				assert.NoError(t, err)
				assert.Empty(t, password)
			}
			// Note: File backend tests would require interactive terminal simulation
		})
	}
}

func TestIsSecureEnvironment(t *testing.T) {
	secCheck := IsSecureEnvironment()

	// Basic structure validation
	assert.NotNil(t, secCheck)
	assert.NotNil(t, secCheck.Recommendations)

	// Environment check should detect non-interactive in test environment
	// This is expected in CI/testing environments
	t.Logf("Interactive: %v", secCheck.IsInteractive)
	t.Logf("Secure Input: %v", secCheck.HasSecureInput)
	t.Logf("Environment Safe: %v", secCheck.EnvironmentSafe)
	t.Logf("Recommendations: %v", secCheck.Recommendations)

	// Should always have some basic structure
	assert.IsType(t, bool(false), secCheck.IsInteractive)
	assert.IsType(t, bool(false), secCheck.HasSecureInput)
	assert.IsType(t, bool(false), secCheck.EnvironmentSafe)
}

func TestPasswordManager(t *testing.T) {
	pm := NewPasswordManager(false, "")

	// Test password manager creation
	assert.NotNil(t, pm)
	assert.False(t, pm.useFileCache)
	assert.Empty(t, pm.cacheFile)

	// Test with file cache
	pmWithCache := NewPasswordManager(true, "/tmp/cache")
	assert.True(t, pmWithCache.useFileCache)
	assert.Equal(t, "/tmp/cache", pmWithCache.cacheFile)
}
