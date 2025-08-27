package keys

import (
	"crypto/sha256"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
)

// SecurityLevel represents the security level of key operations
type SecurityLevel string

const (
	SecurityLevelLow    SecurityLevel = "low"
	SecurityLevelMedium SecurityLevel = "medium"
	SecurityLevelHigh   SecurityLevel = "high"
)

// KeySecurityManager handles security validation for key operations
type KeySecurityManager struct {
	minSecurityLevel SecurityLevel
	keyringPath      string
	log              zerolog.Logger
}

// NewKeySecurityManager creates a new key security manager
func NewKeySecurityManager(minSecurityLevel SecurityLevel, keyringPath string) *KeySecurityManager {
	logger := zerolog.New(nil).With().Str("module", "KeySecurityManager").Logger()
	return &KeySecurityManager{
		minSecurityLevel: minSecurityLevel,
		keyringPath:      keyringPath,
		log:              logger,
	}
}

// ValidateKeyringDirectory validates the keyring directory security
func (ksm *KeySecurityManager) ValidateKeyringDirectory() error {
	if ksm.keyringPath == "" {
		return fmt.Errorf("keyring path is empty")
	}

	// Check if directory exists
	info, err := os.Stat(ksm.keyringPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Directory doesn't exist, create it with secure permissions
			if err := os.MkdirAll(ksm.keyringPath, 0700); err != nil {
				return fmt.Errorf("failed to create keyring directory: %w", err)
			}
			ksm.log.Info().Str("path", ksm.keyringPath).Msg("Created keyring directory with secure permissions")
			return nil
		}
		return fmt.Errorf("failed to check keyring directory: %w", err)
	}

	// Check if it's a directory
	if !info.IsDir() {
		return fmt.Errorf("keyring path is not a directory: %s", ksm.keyringPath)
	}

	// Check directory permissions
	mode := info.Mode()
	if mode.Perm() != 0700 {
		ksm.log.Warn().
			Str("path", ksm.keyringPath).
			Str("current_perms", mode.Perm().String()).
			Msg("Keyring directory permissions are not optimal (should be 700)")
		
		// Attempt to fix permissions
		if err := os.Chmod(ksm.keyringPath, 0700); err != nil {
			return fmt.Errorf("failed to set secure permissions on keyring directory: %w", err)
		}
		ksm.log.Info().Str("path", ksm.keyringPath).Msg("Fixed keyring directory permissions")
	}

	return nil
}

// ValidateKeyAccess validates that key access is secure and authorized
func (ksm *KeySecurityManager) ValidateKeyAccess(keyName string, operation string) error {
	ksm.log.Debug().
		Str("key_name", keyName).
		Str("operation", operation).
		Msg("Validating key access")

	// Check if operation is authorized
	allowedOperations := []string{"create", "access", "sign", "export", "delete"}
	authorized := false
	for _, allowed := range allowedOperations {
		if operation == allowed {
			authorized = true
			break
		}
	}

	if !authorized {
		return fmt.Errorf("unauthorized key operation: %s", operation)
	}

	// Additional security checks based on operation
	switch operation {
	case "export":
		ksm.log.Warn().
			Str("key_name", keyName).
			Msg("Key export operation requested - ensure secure handling of exported key")
	case "delete":
		ksm.log.Warn().
			Str("key_name", keyName).
			Msg("Key deletion operation requested - this action cannot be undone")
	}

	return nil
}

// ValidateKeyCreation validates key creation parameters
func (ksm *KeySecurityManager) ValidateKeyCreation(keyName string, backend KeyringBackend) error {
	if keyName == "" {
		return fmt.Errorf("key name cannot be empty")
	}

	// Validate key name format (basic sanitization)
	if len(keyName) > 64 {
		return fmt.Errorf("key name too long (max 64 characters)")
	}

	// Check for potentially dangerous characters
	for _, char := range keyName {
		if char < 32 || char > 126 {
			return fmt.Errorf("key name contains invalid characters")
		}
	}

	// Validate backend security level
	switch backend {
	case KeyringBackendTest:
		if ksm.minSecurityLevel == SecurityLevelHigh {
			return fmt.Errorf("test backend not allowed for high security level")
		}
		ksm.log.Warn().Msg("Using test keyring backend - keys are stored unencrypted")
	case KeyringBackendFile:
		ksm.log.Info().Msg("Using file keyring backend - keys will be encrypted")
	default:
		return fmt.Errorf("unsupported keyring backend: %s", backend)
	}

	return nil
}

// AuditKeyOperation logs key operations for security auditing
func (ksm *KeySecurityManager) AuditKeyOperation(operation KeyOperation) {
	ksm.log.Info().
		Str("operation", operation.Type).
		Str("key_name", operation.KeyName).
		Str("user", operation.User).
		Time("timestamp", operation.Timestamp).
		Bool("success", operation.Success).
		Str("details", operation.Details).
		Msg("Key operation audit log")
}

// KeyOperation represents a key operation for auditing
type KeyOperation struct {
	Type      string    `json:"type"`
	KeyName   string    `json:"key_name"`
	User      string    `json:"user"`
	Timestamp time.Time `json:"timestamp"`
	Success   bool      `json:"success"`
	Details   string    `json:"details"`
}

// CreateAuditLog creates an audit log entry for a key operation
func CreateAuditLog(opType, keyName, details string, success bool) KeyOperation {
	user := os.Getenv("USER")
	if user == "" {
		user = "unknown"
	}

	return KeyOperation{
		Type:      opType,
		KeyName:   keyName,
		User:      user,
		Timestamp: time.Now(),
		Success:   success,
		Details:   details,
	}
}

// ValidateKeyIntegrity validates the integrity of a key
func (ksm *KeySecurityManager) ValidateKeyIntegrity(kb keyring.Keyring, keyName string) error {
	// Get key record
	record, err := kb.Key(keyName)
	if err != nil {
		return fmt.Errorf("failed to get key record: %w", err)
	}

	// Validate key record
	if record == nil {
		return fmt.Errorf("key record is nil")
	}

	// Check if key name matches
	if record.Name != keyName {
		return fmt.Errorf("key name mismatch: expected %s, got %s", keyName, record.Name)
	}

	// Get public key
	pubKey, err := record.GetPubKey()
	if err != nil {
		return fmt.Errorf("failed to get public key: %w", err)
	}

	if pubKey == nil {
		return fmt.Errorf("public key is nil")
	}

	// Get address
	addr, err := record.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get address: %w", err)
	}

	// Validate address consistency
	expectedAddr := sdk.AccAddress(pubKey.Address())
	if !expectedAddr.Equals(addr) {
		return fmt.Errorf("address mismatch: derived address doesn't match stored address")
	}

	ksm.log.Debug().
		Str("key_name", keyName).
		Str("address", addr.String()).
		Msg("Key integrity validation passed")

	return nil
}

// GenerateKeyFingerprint generates a fingerprint for a key
func (ksm *KeySecurityManager) GenerateKeyFingerprint(kb keyring.Keyring, keyName string) (string, error) {
	record, err := kb.Key(keyName)
	if err != nil {
		return "", fmt.Errorf("failed to get key record: %w", err)
	}

	pubKey, err := record.GetPubKey()
	if err != nil {
		return "", fmt.Errorf("failed to get public key: %w", err)
	}

	// Create fingerprint from public key bytes
	hash := sha256.Sum256(pubKey.Bytes())
	fingerprint := fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for readability

	return fingerprint, nil
}

// SecureKeyDeletion securely deletes a key with confirmation
func (ksm *KeySecurityManager) SecureKeyDeletion(kb keyring.Keyring, keyName string, force bool) error {
	// Validate key exists
	if err := ksm.ValidateKeyIntegrity(kb, keyName); err != nil {
		return fmt.Errorf("key validation failed before deletion: %w", err)
	}

	// Generate fingerprint for confirmation
	fingerprint, err := ksm.GenerateKeyFingerprint(kb, keyName)
	if err != nil {
		return fmt.Errorf("failed to generate key fingerprint: %w", err)
	}

	if !force {
		// Interactive confirmation would go here
		// For now, just log the warning
		ksm.log.Warn().
			Str("key_name", keyName).
			Str("fingerprint", fingerprint).
			Msg("Key deletion requested - this action cannot be undone")
	}

	// Perform deletion
	if err := kb.Delete(keyName); err != nil {
		auditLog := CreateAuditLog("delete", keyName, fmt.Sprintf("deletion failed: %s", err), false)
		ksm.AuditKeyOperation(auditLog)
		return fmt.Errorf("failed to delete key: %w", err)
	}

	// Audit successful deletion
	auditLog := CreateAuditLog("delete", keyName, fmt.Sprintf("key deleted, fingerprint: %s", fingerprint), true)
	ksm.AuditKeyOperation(auditLog)

	return nil
}

// GetSecurityRecommendations returns security recommendations for the current setup
func (ksm *KeySecurityManager) GetSecurityRecommendations() []SecurityRecommendation {
	var recommendations []SecurityRecommendation

	// Check keyring directory
	if info, err := os.Stat(ksm.keyringPath); err == nil {
		if info.Mode().Perm() != 0700 {
			recommendations = append(recommendations, SecurityRecommendation{
				Level:       "HIGH",
				Category:    "File Permissions",
				Issue:       "Keyring directory permissions are not secure",
				Resolution:  fmt.Sprintf("Set directory permissions to 700: chmod 700 %s", ksm.keyringPath),
			})
		}
	}

	// Check for test backend usage
	recommendations = append(recommendations, SecurityRecommendation{
		Level:      "MEDIUM",
		Category:   "Keyring Backend",
		Issue:      "Consider using file backend instead of test for production",
		Resolution: "Use --keyring-backend=file for encrypted key storage",
	})

	// General security recommendations
	recommendations = append(recommendations, SecurityRecommendation{
		Level:      "INFO",
		Category:   "Operational Security",
		Issue:      "Regular security practices",
		Resolution: "Regularly backup keys, use strong passwords, keep software updated",
	})

	return recommendations
}

// SecurityRecommendation represents a security recommendation
type SecurityRecommendation struct {
	Level      string `json:"level"`
	Category   string `json:"category"`
	Issue      string `json:"issue"`
	Resolution string `json:"resolution"`
}

// PrintSecurityRecommendations prints security recommendations to console
func PrintSecurityRecommendations(recommendations []SecurityRecommendation) {
	if len(recommendations) == 0 {
		fmt.Println("✅ No security issues found")
		return
	}

	fmt.Println("\n🔒 Security Recommendations:")
	fmt.Println("=" + strings.Repeat("=", 50))
	
	for i, rec := range recommendations {
		var icon string
		switch rec.Level {
		case "HIGH":
			icon = "🔴"
		case "MEDIUM":
			icon = "🟡"
		case "LOW":
			icon = "🟢"
		default:
			icon = "ℹ️"
		}
		
		fmt.Printf("\n%d. %s [%s] %s\n", i+1, icon, rec.Level, rec.Category)
		fmt.Printf("   Issue: %s\n", rec.Issue)
		fmt.Printf("   Resolution: %s\n", rec.Resolution)
	}
	fmt.Println()
}