package keys

import (
	"os"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/config"
)

func TestMain(m *testing.M) {
	sdkConfig := sdk.GetConfig()
	func() {
		defer func() { _ = recover() }()
		sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
		sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
		sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
		sdkConfig.Seal()
	}()

	os.Exit(m.Run())
}

func TestNewKeys(t *testing.T) {
	tempDir := t.TempDir()
	kb, err := CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	k := NewKeys(kb, "test-hotkey")

	require.NotNil(t, k)
	assert.Equal(t, "test-hotkey", k.GetKeyName())
}

func TestKeyringBackends(t *testing.T) {
	t.Run("test backend", func(t *testing.T) {
		kb, err := CreateKeyring(t.TempDir(), nil, config.KeyringBackendTest)
		require.NoError(t, err)
		assert.Equal(t, "test", kb.Backend())
	})

	t.Run("file backend", func(t *testing.T) {
		kb, err := CreateKeyring(t.TempDir(), nil, config.KeyringBackendFile)
		require.NoError(t, err)
		assert.Equal(t, "file", kb.Backend())
	})

	t.Run("empty home dir", func(t *testing.T) {
		_, err := CreateKeyring("", nil, config.KeyringBackendTest)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "home directory is empty")
	})
}

func TestKeysWithFileBackend(t *testing.T) {
	tempDir := t.TempDir()

	passwordReader := strings.NewReader("testpass\ntestpass\n")
	kb, err := CreateKeyring(tempDir, passwordReader, config.KeyringBackendFile)
	require.NoError(t, err)

	_, _, err = CreateNewKey(kb, "test-key", "", "testpass")
	require.NoError(t, err)

	k := NewKeys(kb, "test-key")

	kr, err := k.GetKeyring()
	require.NoError(t, err)
	assert.Equal(t, kb.Backend(), kr.Backend())
}

func TestCreateNewKey(t *testing.T) {
	tempDir := t.TempDir()
	kb, err := CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	t.Run("generate new key", func(t *testing.T) {
		record, mnemonic, err := CreateNewKey(kb, "new-key", "", "")
		require.NoError(t, err)
		assert.NotNil(t, record)
		assert.Equal(t, "new-key", record.Name)
		assert.NotEmpty(t, mnemonic)
	})

	t.Run("import from mnemonic", func(t *testing.T) {
		mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
		record, returnedMnemonic, err := CreateNewKey(kb, "mnemonic-key", mnemonic, "")
		require.NoError(t, err)
		assert.Equal(t, "mnemonic-key", record.Name)
		assert.Equal(t, mnemonic, returnedMnemonic)
	})

	t.Run("invalid mnemonic", func(t *testing.T) {
		_, _, err := CreateNewKey(kb, "bad-key", "invalid mnemonic words", "")
		require.Error(t, err)
	})
}

func TestGetAddress(t *testing.T) {
	tempDir := t.TempDir()
	kb, err := CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	record, _, err := CreateNewKey(kb, "addr-test", "", "")
	require.NoError(t, err)

	t.Run("valid key", func(t *testing.T) {
		k := NewKeys(kb, record.Name)
		addr, err := k.GetAddress()
		require.NoError(t, err)
		assert.NotEmpty(t, addr)
	})

	t.Run("non-existent key", func(t *testing.T) {
		k := NewKeys(kb, "non-existent")
		_, err := k.GetAddress()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get key")
	})
}

func TestGetKeyring(t *testing.T) {
	tempDir := t.TempDir()
	kb, err := CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	record, _, err := CreateNewKey(kb, "kr-test", "", "")
	require.NoError(t, err)

	t.Run("valid key", func(t *testing.T) {
		k := NewKeys(kb, record.Name)
		kr, err := k.GetKeyring()
		require.NoError(t, err)
		assert.NotNil(t, kr)
	})

	t.Run("non-existent key", func(t *testing.T) {
		k := NewKeys(kb, "non-existent")
		kr, err := k.GetKeyring()
		require.Error(t, err)
		assert.Nil(t, kr)
		assert.Contains(t, err.Error(), "not found in keyring")
	})
}

func TestConcurrentKeyAccess(t *testing.T) {
	tempDir := t.TempDir()
	kb, err := CreateKeyring(tempDir, nil, config.KeyringBackendTest)
	require.NoError(t, err)

	_, _, err = CreateNewKey(kb, "concurrent-key", "", "")
	require.NoError(t, err)

	k := NewKeys(kb, "concurrent-key")

	const numGoroutines = 10
	results := make(chan error, numGoroutines)

	for i := range numGoroutines {
		_ = i
		go func() {
			_, err := k.GetAddress()
			results <- err
		}()
	}

	for range numGoroutines {
		assert.NoError(t, <-results)
	}
}
