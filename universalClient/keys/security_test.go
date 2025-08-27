package keys

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeySecurityManager(t *testing.T) {
	ksm := NewKeySecurityManager(SecurityLevelMedium, "/tmp/test")
	
	assert.NotNil(t, ksm)
	assert.Equal(t, SecurityLevelMedium, ksm.minSecurityLevel)
	assert.Equal(t, "/tmp/test", ksm.keyringPath)
	assert.NotNil(t, ksm.log)
}

func TestValidateKeyringDirectory(t *testing.T) {
	// Create temporary directory for testing
	tempDir, err := os.MkdirTemp("", "keyring-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	ksm := NewKeySecurityManager(SecurityLevelMedium, tempDir)

	// Test directory validation - should pass and set correct permissions
	err = ksm.ValidateKeyringDirectory()
	assert.NoError(t, err)

	// Check permissions
	info, err := os.Stat(tempDir)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0700), info.Mode().Perm())

	// Test with empty path
	ksmEmpty := NewKeySecurityManager(SecurityLevelMedium, "")
	err = ksmEmpty.ValidateKeyringDirectory()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "keyring path is empty")

	// Test with file instead of directory
	tempFile := filepath.Join(tempDir, "testfile")
	err = os.WriteFile(tempFile, []byte("test"), 0644)
	require.NoError(t, err)

	ksmFile := NewKeySecurityManager(SecurityLevelMedium, tempFile)
	err = ksmFile.ValidateKeyringDirectory()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not a directory")
}

func TestValidateKeyAccess(t *testing.T) {
	ksm := NewKeySecurityManager(SecurityLevelMedium, "/tmp/test")

	tests := []struct {
		name      string
		keyName   string
		operation string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid create operation",
			keyName:   "test-key",
			operation: "create",
			wantErr:   false,
		},
		{
			name:      "valid access operation",
			keyName:   "test-key",
			operation: "access",
			wantErr:   false,
		},
		{
			name:      "valid sign operation",
			keyName:   "test-key",
			operation: "sign",
			wantErr:   false,
		},
		{
			name:      "valid export operation",
			keyName:   "test-key",
			operation: "export",
			wantErr:   false,
		},
		{
			name:      "valid delete operation",
			keyName:   "test-key",
			operation: "delete",
			wantErr:   false,
		},
		{
			name:      "invalid operation",
			keyName:   "test-key",
			operation: "invalid",
			wantErr:   true,
			errMsg:    "unauthorized key operation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ksm.ValidateKeyAccess(tt.keyName, tt.operation)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateKeyCreation(t *testing.T) {
	ksm := NewKeySecurityManager(SecurityLevelMedium, "/tmp/test")

	tests := []struct {
		name    string
		keyName string
		backend KeyringBackend
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid key creation with file backend",
			keyName: "test-key",
			backend: KeyringBackendFile,
			wantErr: false,
		},
		{
			name:    "valid key creation with test backend",
			keyName: "test-key",
			backend: KeyringBackendTest,
			wantErr: false,
		},
		{
			name:    "empty key name",
			keyName: "",
			backend: KeyringBackendFile,
			wantErr: true,
			errMsg:  "key name cannot be empty",
		},
		{
			name:    "key name too long",
			keyName: "a" + string(make([]byte, 64)), // 65 characters
			backend: KeyringBackendFile,
			wantErr: true,
			errMsg:  "key name too long",
		},
		{
			name:    "invalid backend",
			keyName: "test-key",
			backend: "invalid",
			wantErr: true,
			errMsg:  "unsupported keyring backend",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ksm.ValidateKeyCreation(tt.keyName, tt.backend)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHighSecurityLevelRejectsTestBackend(t *testing.T) {
	ksmHigh := NewKeySecurityManager(SecurityLevelHigh, "/tmp/test")

	err := ksmHigh.ValidateKeyCreation("test-key", KeyringBackendTest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "test backend not allowed for high security level")
}

func TestCreateAuditLog(t *testing.T) {
	// Set a known user for testing
	originalUser := os.Getenv("USER")
	os.Setenv("USER", "testuser")
	defer os.Setenv("USER", originalUser)

	auditLog := CreateAuditLog("create", "test-key", "key created successfully", true)

	assert.Equal(t, "create", auditLog.Type)
	assert.Equal(t, "test-key", auditLog.KeyName)
	assert.Equal(t, "testuser", auditLog.User)
	assert.True(t, auditLog.Success)
	assert.Equal(t, "key created successfully", auditLog.Details)
	assert.WithinDuration(t, time.Now(), auditLog.Timestamp, time.Second)
}

func TestAuditKeyOperation(t *testing.T) {
	ksm := NewKeySecurityManager(SecurityLevelMedium, "/tmp/test")

	auditLog := CreateAuditLog("test", "test-key", "test operation", true)

	// This should not panic
	ksm.AuditKeyOperation(auditLog)
}


func TestPrintSecurityRecommendations(t *testing.T) {
	recommendations := []SecurityRecommendation{
		{
			Level:      "HIGH",
			Category:   "Test Category",
			Issue:      "Test issue",
			Resolution: "Test resolution",
		},
		{
			Level:      "MEDIUM",
			Category:   "Another Category",
			Issue:      "Another issue",
			Resolution: "Another resolution",
		},
	}

	// This should not panic
	PrintSecurityRecommendations(recommendations)

	// Test with empty recommendations
	PrintSecurityRecommendations([]SecurityRecommendation{})
}

func TestSecurityLevels(t *testing.T) {
	// Test that security levels are properly defined
	assert.Equal(t, SecurityLevel("low"), SecurityLevelLow)
	assert.Equal(t, SecurityLevel("medium"), SecurityLevelMedium)
	assert.Equal(t, SecurityLevel("high"), SecurityLevelHigh)
}
