package keyshare

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

var (
	ErrKeyshareNotFound = errors.New("keyshare not found")
	ErrInvalidKeyID     = errors.New("invalid key ID")
	ErrInvalidKey       = errors.New("invalid encryption key")
	ErrDecryptionFailed = errors.New("decryption failed")
)

const (
	keysharesDirName = "keyshares"
	filePerms        = 0o600 // Read/write for owner only
	dirPerms         = 0o700 // Read/write/execute for owner only

	// Encryption constants
	saltLength       = 32
	nonceLength      = 12 // GCM nonce length
	keyLength        = 32 // AES-256 key length
	pbkdf2Iterations = 100000
)

// Manager provides methods for storing and retrieving encrypted keyshares from files.
type Manager struct {
	keysharesDir string
	password     string // Password for encryption/decryption
}

// NewManager creates a new keyshare manager instance.
// homeDir: Base directory (e.g., $HOME/.puniversal)
// encryptionPassword: Password for encrypting/decrypting keyshares
func NewManager(homeDir string, encryptionPassword string) (*Manager, error) {
	if homeDir == "" {
		return nil, errors.New("home directory cannot be empty")
	}

	keysharesDir := filepath.Join(homeDir, keysharesDirName)

	// Create keyshares directory if it doesn't exist
	if err := os.MkdirAll(keysharesDir, dirPerms); err != nil {
		return nil, fmt.Errorf("failed to create keyshares directory: %w", err)
	}

	return &Manager{
		keysharesDir: keysharesDir,
		password:     encryptionPassword,
	}, nil
}

// Store stores an encrypted keyshare as a file.
// keyshareBytes: Raw keyshare bytes from DKLS library
// keyID: Unique key identifier (used as filename)
func (m *Manager) Store(keyshareBytes []byte, keyID string) error {
	if keyID == "" {
		return ErrInvalidKeyID
	}

	// Validate keyID doesn't contain path separators or other dangerous characters
	if strings.Contains(keyID, "/") || strings.Contains(keyID, "\\") || strings.Contains(keyID, "..") {
		return fmt.Errorf("%w: keyID contains invalid characters", ErrInvalidKeyID)
	}

	// Encrypt keyshare
	encryptedData, err := m.encrypt(keyshareBytes)
	if err != nil {
		return fmt.Errorf("failed to encrypt keyshare: %w", err)
	}

	// Write to file
	filePath := filepath.Join(m.keysharesDir, keyID)
	if err := os.WriteFile(filePath, encryptedData, filePerms); err != nil {
		return fmt.Errorf("failed to write keyshare file: %w", err)
	}

	return nil
}

// Get retrieves and decrypts a keyshare from a file.
// Returns the decrypted keyshare bytes.
func (m *Manager) Get(keyID string) ([]byte, error) {
	if keyID == "" {
		return nil, ErrInvalidKeyID
	}

	// Validate keyID doesn't contain path separators
	if strings.Contains(keyID, "/") || strings.Contains(keyID, "\\") || strings.Contains(keyID, "..") {
		return nil, fmt.Errorf("%w: keyID contains invalid characters", ErrInvalidKeyID)
	}

	filePath := filepath.Join(m.keysharesDir, keyID)

	// Read encrypted file
	encryptedData, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrKeyshareNotFound
		}
		return nil, fmt.Errorf("failed to read keyshare file: %w", err)
	}

	// Decrypt keyshare
	keyshareBytes, err := m.decrypt(encryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt keyshare: %w", err)
	}

	return keyshareBytes, nil
}

// Exists checks if a keyshare file exists for the given keyID.
func (m *Manager) Exists(keyID string) (bool, error) {
	if keyID == "" {
		return false, ErrInvalidKeyID
	}

	// Validate keyID doesn't contain path separators
	if strings.Contains(keyID, "/") || strings.Contains(keyID, "\\") || strings.Contains(keyID, "..") {
		return false, fmt.Errorf("%w: keyID contains invalid characters", ErrInvalidKeyID)
	}

	filePath := filepath.Join(m.keysharesDir, keyID)
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check keyshare file: %w", err)
	}

	return true, nil
}

// List returns all keyshare keyIDs (filenames) in the keyshares directory.
func (m *Manager) List() ([]string, error) {
	entries, err := os.ReadDir(m.keysharesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read keyshares directory: %w", err)
	}

	keyIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		// Only include regular files (not directories)
		if !entry.IsDir() {
			keyIDs = append(keyIDs, entry.Name())
		}
	}

	return keyIDs, nil
}

// encrypt encrypts keyshare data using AES-256-GCM with a password-derived key.
// Returns encrypted data in format: [salt(32) || nonce(12) || ciphertext || tag(16)]
func (m *Manager) encrypt(keyshareData []byte) ([]byte, error) {
	if len(keyshareData) == 0 {
		return nil, errors.New("keyshare data cannot be empty")
	}

	// Generate random salt
	salt := make([]byte, saltLength)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("failed to generate salt: %w", err)
	}

	// Derive encryption key from password
	key := pbkdf2.Key([]byte(m.password), salt, pbkdf2Iterations, keyLength, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Generate random nonce
	nonce := make([]byte, nonceLength)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt
	ciphertext := gcm.Seal(nonce, nonce, keyshareData, nil)

	// Combine: salt || encrypted_data (nonce || ciphertext || tag)
	encrypted := make([]byte, 0, saltLength+len(ciphertext))
	encrypted = append(encrypted, salt...)
	encrypted = append(encrypted, ciphertext...)

	return encrypted, nil
}

// decrypt decrypts keyshare data encrypted with encrypt.
func (m *Manager) decrypt(encryptedData []byte) ([]byte, error) {
	if len(encryptedData) < saltLength+nonceLength {
		return nil, ErrDecryptionFailed
	}

	// Extract salt and ciphertext
	salt := encryptedData[:saltLength]
	ciphertext := encryptedData[saltLength:]

	// Derive decryption key from password
	key := pbkdf2.Key([]byte(m.password), salt, pbkdf2Iterations, keyLength, sha256.New)

	// Create cipher
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// Extract nonce (first nonceLength bytes of ciphertext)
	if len(ciphertext) < nonceLength {
		return nil, ErrDecryptionFailed
	}
	nonce := ciphertext[:nonceLength]
	ciphertextOnly := ciphertext[nonceLength:]

	// Decrypt
	plaintext, err := gcm.Open(nil, nonce, ciphertextOnly, nil)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}
