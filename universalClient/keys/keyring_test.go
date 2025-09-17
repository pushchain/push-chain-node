package keys

import (
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	// Create keyring with EVM compatibility using our standard CreateKeyring function
	suite.kb, err = CreateKeyring(suite.tempDir, nil, KeyringBackendTest)
	require.NoError(suite.T(), err, "keyring creation should succeed")
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

	kb, record, err := GetKeyringKeybase(suite.config)

	// Should succeed now with proper keyring setup
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), kb)
	assert.NotEqual(suite.T(), "", record)
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
	_, _, err = GetKeyringKeybase(validConfig)
	assert.NoError(suite.T(), err) // Should succeed with proper keyring setup
}

// Run the test suite
func TestKeyring(t *testing.T) {
	suite.Run(t, new(KeyringTestSuite))
}
