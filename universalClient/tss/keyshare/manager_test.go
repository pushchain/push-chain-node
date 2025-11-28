package keyshare

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		password := "test-password-123"

		mgr, err := NewManager(tmpDir, password)
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}
		if mgr == nil {
			t.Fatal("NewManager() returned nil manager")
		}
		if mgr.keysharesDir != filepath.Join(tmpDir, keysharesDirName) {
			t.Errorf("NewManager() keysharesDir = %v, want %v", mgr.keysharesDir, filepath.Join(tmpDir, keysharesDirName))
		}
		if mgr.password != password {
			t.Errorf("NewManager() password = %v, want %v", mgr.password, password)
		}

		// Verify directory was created
		if _, err := os.Stat(mgr.keysharesDir); os.IsNotExist(err) {
			t.Errorf("keyshares directory was not created: %v", err)
		}
	})

	t.Run("empty homeDir", func(t *testing.T) {
		mgr, err := NewManager("", "password")
		if err == nil {
			t.Fatal("NewManager() error = nil, want error")
		}
		if mgr != nil {
			t.Fatal("NewManager() returned non-nil manager on error")
		}
	})

	t.Run("creates directory with correct permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v, want nil", err)
		}

		info, err := os.Stat(mgr.keysharesDir)
		if err != nil {
			t.Fatalf("failed to stat keyshares directory: %v", err)
		}

		// Check permissions (should be 0700)
		expectedPerms := os.FileMode(dirPerms)
		if info.Mode().Perm() != expectedPerms {
			t.Errorf("keyshares directory permissions = %v, want %v", info.Mode().Perm(), expectedPerms)
		}
	})
}

func TestManager_Store(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "test-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key-1"
		keyshareData := []byte("test keyshare data")

		err = mgr.Store(keyshareData, keyID)
		if err != nil {
			t.Fatalf("Store() error = %v, want nil", err)
		}

		// Verify file was created
		filePath := filepath.Join(mgr.keysharesDir, keyID)
		if _, err := os.Stat(filePath); os.IsNotExist(err) {
			t.Fatal("keyshare file was not created")
		}

		// Verify file has correct permissions
		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("failed to stat keyshare file: %v", err)
		}
		expectedPerms := os.FileMode(filePerms)
		if info.Mode().Perm() != expectedPerms {
			t.Errorf("keyshare file permissions = %v, want %v", info.Mode().Perm(), expectedPerms)
		}
	})

	t.Run("empty keyID", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		err = mgr.Store([]byte("data"), "")
		if err != ErrInvalidKeyID {
			t.Errorf("Store() error = %v, want %v", err, ErrInvalidKeyID)
		}
	})

	t.Run("keyID with path separator", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		testCases := []string{
			"key/../id",
			"key\\id",
			"../key",
			"key/../../etc/passwd",
		}

		for _, keyID := range testCases {
			err = mgr.Store([]byte("data"), keyID)
			if err == nil {
				t.Errorf("Store() with keyID %q error = nil, want error", keyID)
			}
			if err != ErrInvalidKeyID && !reflect.TypeOf(err).AssignableTo(reflect.TypeOf(&ErrInvalidKeyID)) {
				// Check if it's a wrapped ErrInvalidKeyID
				if !reflect.TypeOf(err).AssignableTo(reflect.TypeOf((*error)(nil)).Elem()) {
					t.Errorf("Store() with keyID %q error = %v, want ErrInvalidKeyID", keyID, err)
				}
			}
		}
	})

	t.Run("encryption works", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "test-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyshareData := []byte("sensitive keyshare data")
		keyID := "test-key"

		err = mgr.Store(keyshareData, keyID)
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}

		// Read raw file and verify it's encrypted (not plaintext)
		filePath := filepath.Join(mgr.keysharesDir, keyID)
		encryptedData, err := os.ReadFile(filePath)
		if err != nil {
			t.Fatalf("failed to read encrypted file: %v", err)
		}

		// Encrypted data should not contain plaintext
		if string(encryptedData) == string(keyshareData) {
			t.Error("encrypted data matches plaintext - encryption failed")
		}

		// Encrypted data should be longer than plaintext (salt + nonce + ciphertext + tag)
		if len(encryptedData) <= len(keyshareData) {
			t.Errorf("encrypted data length = %d, want > %d", len(encryptedData), len(keyshareData))
		}
	})
}

func TestManager_Get(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "test-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key-1"
		originalData := []byte("test keyshare data")

		// Store keyshare
		err = mgr.Store(originalData, keyID)
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}

		// Retrieve keyshare
		retrievedData, err := mgr.Get(keyID)
		if err != nil {
			t.Fatalf("Get() error = %v, want nil", err)
		}

		if !reflect.DeepEqual(retrievedData, originalData) {
			t.Errorf("Get() data = %v, want %v", retrievedData, originalData)
		}
	})

	t.Run("keyshare not found", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.Get("non-existent-key")
		if err != ErrKeyshareNotFound {
			t.Errorf("Get() error = %v, want %v", err, ErrKeyshareNotFound)
		}
	})

	t.Run("empty keyID", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.Get("")
		if err != ErrInvalidKeyID {
			t.Errorf("Get() error = %v, want %v", err, ErrInvalidKeyID)
		}
	})

	t.Run("keyID with path separator", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.Get("key/../id")
		if err == nil {
			t.Error("Get() error = nil, want error")
		}
	})

	t.Run("wrong password", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr1, err := NewManager(tmpDir, "correct-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key"
		keyshareData := []byte("test data")

		// Store with correct password
		err = mgr1.Store(keyshareData, keyID)
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}

		// Try to retrieve with wrong password
		mgr2, err := NewManager(tmpDir, "wrong-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr2.Get(keyID)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("Get() with wrong password error = %v, want %v", err, ErrDecryptionFailed)
		}
	})

	t.Run("corrupted encrypted data", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key"
		filePath := filepath.Join(mgr.keysharesDir, keyID)

		// Write corrupted data
		corruptedData := []byte("corrupted encrypted data")
		err = os.WriteFile(filePath, corruptedData, filePerms)
		if err != nil {
			t.Fatalf("failed to write corrupted file: %v", err)
		}

		_, err = mgr.Get(keyID)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("Get() with corrupted data error = %v, want %v", err, ErrDecryptionFailed)
		}
	})

	t.Run("too short encrypted data", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key"
		filePath := filepath.Join(mgr.keysharesDir, keyID)

		// Write data that's too short (less than saltLength + nonceLength)
		shortData := make([]byte, saltLength+nonceLength-1)
		err = os.WriteFile(filePath, shortData, filePerms)
		if err != nil {
			t.Fatalf("failed to write short file: %v", err)
		}

		_, err = mgr.Get(keyID)
		if !errors.Is(err, ErrDecryptionFailed) {
			t.Errorf("Get() with too short data error = %v, want %v", err, ErrDecryptionFailed)
		}
	})
}

func TestManager_Exists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		keyID := "test-key"
		keyshareData := []byte("test data")

		// Store keyshare
		err = mgr.Store(keyshareData, keyID)
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}

		// Check existence
		exists, err := mgr.Exists(keyID)
		if err != nil {
			t.Fatalf("Exists() error = %v, want nil", err)
		}
		if !exists {
			t.Error("Exists() = false, want true")
		}
	})

	t.Run("does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		exists, err := mgr.Exists("non-existent-key")
		if err != nil {
			t.Fatalf("Exists() error = %v, want nil", err)
		}
		if exists {
			t.Error("Exists() = true, want false")
		}
	})

	t.Run("empty keyID", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.Exists("")
		if err != ErrInvalidKeyID {
			t.Errorf("Exists() error = %v, want %v", err, ErrInvalidKeyID)
		}
	})

	t.Run("keyID with path separator", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.Exists("key/../id")
		if err == nil {
			t.Error("Exists() error = nil, want error")
		}
	})
}

func TestManager_EncryptDecrypt(t *testing.T) {
	t.Run("round-trip encryption", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "test-password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		testCases := [][]byte{
			[]byte("small"),
			[]byte("medium length keyshare data"),
			make([]byte, 1024),  // 1KB
			make([]byte, 10240), // 10KB
		}

		for i, originalData := range testCases {
			// Fill large byte slices with some data
			if len(originalData) > 100 {
				for j := range originalData {
					originalData[j] = byte(j % 256)
				}
			}

			// Encrypt
			encryptedData, err := mgr.encrypt(originalData)
			if err != nil {
				t.Fatalf("encrypt() test case %d error = %v", i, err)
			}

			// Verify encrypted data is different from original
			if reflect.DeepEqual(encryptedData, originalData) {
				t.Errorf("encrypt() test case %d: encrypted data matches original", i)
			}

			// Decrypt
			decryptedData, err := mgr.decrypt(encryptedData)
			if err != nil {
				t.Fatalf("decrypt() test case %d error = %v", i, err)
			}

			// Verify decrypted data matches original
			if !reflect.DeepEqual(decryptedData, originalData) {
				t.Errorf("decrypt() test case %d: decrypted data = %v, want %v", i, decryptedData, originalData)
			}
		}
	})

	t.Run("empty data encryption fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		_, err = mgr.encrypt([]byte{})
		if err == nil {
			t.Error("encrypt() with empty data error = nil, want error")
		}
	})

	t.Run("different passwords produce different ciphertext", func(t *testing.T) {
		tmpDir := t.TempDir()
		originalData := []byte("test keyshare data")

		mgr1, err := NewManager(tmpDir, "password-1")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		mgr2, err := NewManager(tmpDir, "password-2")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		encrypted1, err := mgr1.encrypt(originalData)
		if err != nil {
			t.Fatalf("encrypt() error = %v", err)
		}

		encrypted2, err := mgr2.encrypt(originalData)
		if err != nil {
			t.Fatalf("encrypt() error = %v", err)
		}

		// Encrypted data should be different (due to different salts and keys)
		if reflect.DeepEqual(encrypted1, encrypted2) {
			t.Error("encrypt() with different passwords produced same ciphertext")
		}
	})

	t.Run("same password produces different ciphertext (nonce)", func(t *testing.T) {
		tmpDir := t.TempDir()
		mgr, err := NewManager(tmpDir, "password")
		if err != nil {
			t.Fatalf("NewManager() error = %v", err)
		}

		originalData := []byte("test keyshare data")

		encrypted1, err := mgr.encrypt(originalData)
		if err != nil {
			t.Fatalf("encrypt() error = %v", err)
		}

		encrypted2, err := mgr.encrypt(originalData)
		if err != nil {
			t.Fatalf("encrypt() error = %v", err)
		}

		// Encrypted data should be different (due to random salt and nonce)
		if reflect.DeepEqual(encrypted1, encrypted2) {
			t.Error("encrypt() with same password produced same ciphertext (should be different due to random nonce)")
		}

		// But both should decrypt to the same plaintext
		decrypted1, err := mgr.decrypt(encrypted1)
		if err != nil {
			t.Fatalf("decrypt() error = %v", err)
		}

		decrypted2, err := mgr.decrypt(encrypted2)
		if err != nil {
			t.Fatalf("decrypt() error = %v", err)
		}

		if !reflect.DeepEqual(decrypted1, originalData) {
			t.Error("decrypt() first encryption did not match original")
		}
		if !reflect.DeepEqual(decrypted2, originalData) {
			t.Error("decrypt() second encryption did not match original")
		}
	})
}
