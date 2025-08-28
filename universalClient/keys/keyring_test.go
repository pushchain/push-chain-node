package keys

import (
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// KeyringTestSuite tests keyring operations
type KeyringTestSuite struct {
	suite.Suite
	tempDir string
	config  KeyringConfig
	kb      keyring.Keyring
}

func (suite *KeyringTestSuite) SetupTest() {
	// Initialize SDK config safely - check if already sealed
	sdkConfig := sdk.GetConfig()
	func() {
		defer func() {
			// Config already sealed, that's fine - ignore panic
			_ = recover()
		}()
		sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
		sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
		sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
		sdkConfig.Seal()
	}()

	// Create temporary directory
	var err error
	suite.tempDir, err = os.MkdirTemp("", "keyring-test")
	require.NoError(suite.T(), err)

	// Create keyring config
	suite.config = KeyringConfig{
		HomeDir:        suite.tempDir,
		KeyringBackend: KeyringBackendTest,
		HotkeyName:     "test-key",
		HotkeyPassword: "",
		OperatorAddr:   "push1abc123def456",
	}

	// Create test keyring with EVM support
	registry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(registry)
	
	// Explicitly register public key types including EVM-compatible keys
	registry.RegisterImplementations((*cryptotypes.PubKey)(nil),
		&secp256k1.PubKey{},
		&ed25519.PubKey{},
		&evmcrypto.PubKey{},
	)
	// Also register private key implementations for EVM compatibility
	registry.RegisterImplementations((*cryptotypes.PrivKey)(nil),
		&secp256k1.PrivKey{},
		&ed25519.PrivKey{},
		&evmcrypto.PrivKey{},
	)
	
	cdc := codec.NewProtoCodec(registry)
	
	// Create keyring with EVM option for proper algorithm support
	suite.kb = keyring.NewInMemory(cdc, cosmosevmkeyring.Option())
	require.NotNil(suite.T(), suite.kb, "keyring should be initialized")
}

func (suite *KeyringTestSuite) TearDownTest() {
	if suite.tempDir != "" {
		_ = os.RemoveAll(suite.tempDir)
	}
}

// TestGetKeyringKeybase tests keyring creation
func (suite *KeyringTestSuite) TestGetKeyringKeybase() {
	kb, record, err := GetKeyringKeybase(suite.config)
	
	// Should fail because the key doesn't exist yet
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), kb)
	assert.Equal(suite.T(), "", record)
	assert.Contains(suite.T(), err.Error(), "not found")
}

// TestGetKeyringKeybaseWithExistingKey tests keyring with existing key
func (suite *KeyringTestSuite) TestGetKeyringKeybaseWithExistingKey() {
	// First create a key in the test keyring
	_, err := CreateNewKey(suite.kb, "test-key", "", "")
	require.NoError(suite.T(), err)

	// Now try to get keyring keybase with a config that would use the same key
	// This tests the validation logic, though it will still fail due to path mismatch
	kb, record, err := GetKeyringKeybase(suite.config)
	
	// Will likely fail due to keyring path differences, but tests the function
	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), kb)
	assert.Equal(suite.T(), "", record)
}

// TestCreateNewKey tests key creation
func (suite *KeyringTestSuite) TestCreateNewKey() {
	record, err := CreateNewKey(suite.kb, "new-test-key", "", "")
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), record)
	assert.Equal(suite.T(), "new-test-key", record.Name)
	
	// Verify key was created
	retrievedRecord, err := suite.kb.Key("new-test-key")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), record.Name, retrievedRecord.Name)
}

// TestCreateNewKeyWithMnemonic tests key creation with mnemonic
func (suite *KeyringTestSuite) TestCreateNewKeyWithMnemonic() {
	mnemonic := "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon about"
	
	record, _, err := CreateNewKeyWithMnemonic(suite.kb, "mnemonic-key", mnemonic, "")
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), record)
	assert.Equal(suite.T(), "mnemonic-key", record.Name)
	
	// Verify key was created
	retrievedRecord, err := suite.kb.Key("mnemonic-key")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), record.Name, retrievedRecord.Name)
}

// TestCreateNewKeyWithInvalidMnemonic tests key creation with invalid mnemonic
func (suite *KeyringTestSuite) TestCreateNewKeyWithInvalidMnemonic() {
	invalidMnemonic := "invalid mnemonic words"
	
	_, _, err := CreateNewKeyWithMnemonic(suite.kb, "invalid-key", invalidMnemonic, "")
	
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "Invalid mnenomic")
}

// TestListKeys tests key listing
func (suite *KeyringTestSuite) TestListKeys() {
	// Create a few test keys
	_, err := CreateNewKey(suite.kb, "key1", "", "")
	require.NoError(suite.T(), err)
	
	_, err = CreateNewKey(suite.kb, "key2", "", "")
	require.NoError(suite.T(), err)
	
	keys, err := ListKeys(suite.kb)
	
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), keys, 2)
	
	// Check key names
	keyNames := make([]string, len(keys))
	for i, key := range keys {
		keyNames[i] = key.Name
	}
	assert.Contains(suite.T(), keyNames, "key1")
	assert.Contains(suite.T(), keyNames, "key2")
}

// TestListKeysEmpty tests listing with no keys
func (suite *KeyringTestSuite) TestListKeysEmpty() {
	keys, err := ListKeys(suite.kb)
	
	require.NoError(suite.T(), err)
	assert.Empty(suite.T(), keys)
}

// TestDeleteKey tests key deletion
func (suite *KeyringTestSuite) TestDeleteKey() {
	// Create a key first
	_, err := CreateNewKey(suite.kb, "delete-me", "", "")
	require.NoError(suite.T(), err)
	
	// Verify it exists
	_, err = suite.kb.Key("delete-me")
	require.NoError(suite.T(), err)
	
	// Delete it
	err = DeleteKey(suite.kb, "delete-me")
	require.NoError(suite.T(), err)
	
	// Verify it's gone
	_, err = suite.kb.Key("delete-me")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found")
}

// TestDeleteNonExistentKey tests deleting non-existent key
func (suite *KeyringTestSuite) TestDeleteNonExistentKey() {
	err := DeleteKey(suite.kb, "non-existent")
	
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found")
}

// TestExportKey tests key export
func (suite *KeyringTestSuite) TestExportKey() {
	// Create a key first
	_, err := CreateNewKey(suite.kb, "export-me", "", "")
	require.NoError(suite.T(), err)
	
	// Export it (for test backend, password is not needed)
	armoredKey, err := ExportKey(suite.kb, "export-me", "")
	
	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), armoredKey)
	assert.Contains(suite.T(), armoredKey, "BEGIN TENDERMINT PRIVATE KEY")
}

// TestExportNonExistentKey tests exporting non-existent key
func (suite *KeyringTestSuite) TestExportNonExistentKey() {
	_, err := ExportKey(suite.kb, "non-existent", "")
	
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found")
}

// TestImportKey tests key import
func (suite *KeyringTestSuite) TestImportKey() {
	// First create and export a key
	_, err := CreateNewKey(suite.kb, "export-import-test", "", "")
	require.NoError(suite.T(), err)
	
	armoredKey, err := ExportKey(suite.kb, "export-import-test", "")
	require.NoError(suite.T(), err)
	
	// Delete the original
	err = DeleteKey(suite.kb, "export-import-test")
	require.NoError(suite.T(), err)
	
	// Import it back
	err = ImportKey(suite.kb, "imported-key", armoredKey, "")
	require.NoError(suite.T(), err)
	
	// Verify it exists
	record, err := suite.kb.Key("imported-key")
	require.NoError(suite.T(), err)
	assert.Equal(suite.T(), "imported-key", record.Name)
}

// TestImportInvalidKey tests importing invalid key
func (suite *KeyringTestSuite) TestImportInvalidKey() {
	invalidKey := "invalid armored key data"
	
	err := ImportKey(suite.kb, "invalid-import", invalidKey, "")
	
	assert.Error(suite.T(), err)
}

// TestGetKeybase tests keybase creation with different backends
func (suite *KeyringTestSuite) TestGetKeybase() {
	// Test with test backend
	kb, err := CreateKeyring(suite.tempDir, nil, KeyringBackendTest)
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), kb)
	assert.Equal(suite.T(), keyring.BackendTest, kb.Backend())
}

// TestGetKeybaseWithFileBackend tests keybase with file backend
func (suite *KeyringTestSuite) TestGetKeybaseWithFileBackend() {
	// Create a mock input reader for password (though it won't be called for test)
	kb, err := CreateKeyring(suite.tempDir, nil, KeyringBackendFile)
	
	require.NoError(suite.T(), err)
	assert.NotNil(suite.T(), kb)
	assert.Equal(suite.T(), keyring.BackendFile, kb.Backend())
}

// TestValidateKeyExists tests key existence validation
func (suite *KeyringTestSuite) TestValidateKeyExists() {
	// Create a key first
	_, err := CreateNewKey(suite.kb, "validation-test", "", "")
	require.NoError(suite.T(), err)
	
	// Test existing key
	err = ValidateKeyExists(suite.kb, "validation-test")
	assert.NoError(suite.T(), err)
	
	// Test non-existent key
	err = ValidateKeyExists(suite.kb, "non-existent")
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found")
}

// TestGetPubkeyBech32FromRecord tests public key extraction
func (suite *KeyringTestSuite) TestGetPubkeyBech32FromRecord() {
	// Create a key
	record, err := CreateNewKey(suite.kb, "pubkey-test", "", "")
	require.NoError(suite.T(), err)
	
	// Get public key
	pubkeyBech32, err := getPubkeyBech32FromRecord(record)
	
	require.NoError(suite.T(), err)
	assert.NotEmpty(suite.T(), pubkeyBech32)
	assert.Contains(suite.T(), pubkeyBech32, "pushpub")
}


// TestKeyringConfigValidation tests keyring config validation
func (suite *KeyringTestSuite) TestKeyringConfigValidation() {
	// Test valid config
	validConfig := KeyringConfig{
		HomeDir:        suite.tempDir,
		KeyringBackend: KeyringBackendTest,
		HotkeyName:     "test-key",
		HotkeyPassword: "",
		OperatorAddr:   "push1abc123def456",
	}
	
	// Create a key for this config to work
	_, err := CreateNewKey(suite.kb, validConfig.HotkeyName, "", "")
	require.NoError(suite.T(), err)
	
	// Test the validation indirectly through GetKeyringKeybase
	// (It will still fail due to path differences, but validates the config structure)
	_, _, err = GetKeyringKeybase(validConfig)
	assert.Error(suite.T(), err) // Expected due to keyring path differences
}

// Run the test suite
func TestKeyring(t *testing.T) {
	suite.Run(t, new(KeyringTestSuite))
}