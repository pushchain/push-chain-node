package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	cosmosevmkeyring "github.com/cosmos/evm/crypto/keyring"
	evmcrypto "github.com/cosmos/evm/crypto/ethsecp256k1"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/keys"
)

var (
	keyringBackendFlag string
	recoverFlag        bool
	noBackupFlag       bool
)

// keysCmd returns the keys command with all subcommands
func keysCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keys",
		Short: "Manage Universal Validator hot keys",
		Long: `
The keys commands allow you to manage hot keys for the Universal Validator.
Hot keys are used to sign transactions on behalf of the operator (validator) account.

Available Commands:
  add     Create a new hot key
  list    List all keys  
  show    Show key details
  delete  Delete a key
  export  Export a key for backup
  import  Import a key from backup
`,
	}

	// Add flags
	cmd.PersistentFlags().StringVar(&keyringBackendFlag, "keyring-backend", "file", "Select keyring backend (test|file)")

	// Add subcommands
	cmd.AddCommand(keysAddCmd())
	cmd.AddCommand(keysListCmd())
	cmd.AddCommand(keysShowCmd())
	cmd.AddCommand(keysDeleteCmd())
	cmd.AddCommand(keysExportCmd())
	cmd.AddCommand(keysImportCmd())
	cmd.AddCommand(keysSecurityCmd())

	return cmd
}

// keysAddCmd creates a new hot key
func keysAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "Create a new hot key",
		Long: `
Create a new hot key for Universal Validator operations.
The key will be encrypted and stored in the keyring.

Examples:
  puniversald keys add my-hotkey
  puniversald keys add my-hotkey --keyring-backend test
  puniversald keys add my-hotkey --recover
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyName := args[0]

			// Load config to get home directory and defaults
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				log.Warn().Err(err).Msg("Config not found, using defaults")
				cfg = config.Config{}
				// Set defaults
				if err := config.Save(&cfg, constant.DefaultNodeHome); err != nil {
					log.Warn().Err(err).Msg("Failed to save default config")
				}
			}

			// Use command line flag if provided, otherwise use config
			backend := config.KeyringBackend(keyringBackendFlag)
			if keyringBackendFlag == "file" || keyringBackendFlag == "" {
				backend = config.KeyringBackendFile
			} else if keyringBackendFlag == "test" {
				backend = config.KeyringBackendTest
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Check if key already exists
			if err := keys.ValidateKeyExists(kb, keyName); err == nil {
				return fmt.Errorf("key with name '%s' already exists", keyName)
			}

			var mnemonic string
			var passphrase string

			if recoverFlag {
				// Import from mnemonic
				fmt.Print("Enter your mnemonic phrase: ")
				reader := bufio.NewReader(os.Stdin)
				mnemonic, err = reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read mnemonic: %w", err)
				}
				mnemonic = strings.TrimSpace(mnemonic)
			}

			// Get passphrase for file backend
			if backend == config.KeyringBackendFile {
				passphrase, err = getPassphrase("Enter passphrase for key encryption: ", true)
				if err != nil {
					return fmt.Errorf("failed to get passphrase: %w", err)
				}
			}

			// Create the key
			record, generatedMnemonic, err := keys.CreateNewKeyWithMnemonic(kb, keyName, mnemonic, passphrase)
			if err != nil {
				return fmt.Errorf("failed to create key: %w", err)
			}

			// Get address
			addr, err := record.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get address: %w", err)
			}

			// Get public key
			pubKey, err := record.GetPubKey()
			if err != nil {
				return fmt.Errorf("failed to get public key: %w", err)
			}

			fmt.Printf("✅ Key created successfully!\n")
			fmt.Printf("Name: %s\n", keyName)
			fmt.Printf("Address: %s\n", addr.String())
			fmt.Printf("Public Key: %x\n", pubKey.Bytes())

			if !recoverFlag && !noBackupFlag && mnemonic == "" {
				// Display generated mnemonic for backup
				fmt.Printf("\n⚠️  IMPORTANT: Save this mnemonic phrase securely!\n")
				fmt.Printf("Mnemonic: %s\n", generatedMnemonic)
				fmt.Printf("\nThis is the only time you will see the mnemonic. Keep it safe!\n")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&recoverFlag, "recover", false, "Import key from mnemonic phrase")
	cmd.Flags().BoolVar(&noBackupFlag, "no-backup", false, "Don't display mnemonic for backup")

	return cmd
}

// keysListCmd lists all keys in the keyring
func keysListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all keys in keyring",
		Long:  `List all keys stored in the keyring with their addresses.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use command line flag if provided, otherwise use config
			backend := cfg.KeyringBackend
			if keyringBackendFlag != "file" && keyringBackendFlag != "" {
				backend = config.KeyringBackend(keyringBackendFlag)
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// List all keys
			keyList, err := keys.ListKeys(kb)
			if err != nil {
				return fmt.Errorf("failed to list keys: %w", err)
			}

			if len(keyList) == 0 {
				fmt.Println("No keys found in keyring")
				return nil
			}

			fmt.Printf("Keys in keyring (%s backend):\n\n", backend)
			for _, record := range keyList {
				name := record.Name
				addr, err := record.GetAddress()
				if err != nil {
					fmt.Printf("❌ %s: <error getting address: %v>\n", name, err)
					continue
				}

				fmt.Printf("📋 Name: %s\n", name)
				fmt.Printf("   Address: %s\n", addr.String())
				
				// Show if this is the configured hot key
				if cfg.AuthzHotkey == name {
					fmt.Printf("   🔥 [ACTIVE HOT KEY]\n")
				}
				fmt.Println()
			}

			return nil
		},
	}

	return cmd
}

// keysShowCmd shows details for a specific key
func keysShowCmd() *cobra.Command {
	var addressOnly bool
	var pubkeyOnly bool

	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show key details",
		Long:  `Show detailed information about a specific key.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyName := args[0]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use command line flag if provided, otherwise use config
			backend := cfg.KeyringBackend
			if keyringBackendFlag != "file" && keyringBackendFlag != "" {
				backend = config.KeyringBackend(keyringBackendFlag)
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Get key record
			record, err := kb.Key(keyName)
			if err != nil {
				return fmt.Errorf("key '%s' not found: %w", keyName, err)
			}

			// Get address
			addr, err := record.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get address: %w", err)
			}

			// Get public key
			pubKey, err := record.GetPubKey()
			if err != nil {
				return fmt.Errorf("failed to get public key: %w", err)
			}

			// Display based on flags
			if addressOnly {
				fmt.Println(addr.String())
				return nil
			}

			if pubkeyOnly {
				fmt.Printf("%x", pubKey.Bytes())
				return nil
			}

			// Display full details
			fmt.Printf("Key Details:\n")
			fmt.Printf("Name: %s\n", keyName)
			fmt.Printf("Address: %s\n", addr.String())
			fmt.Printf("Public Key: %x\n", pubKey.Bytes())
			fmt.Printf("Type: %s\n", record.GetType())

			// Show if this is the configured hot key
			if cfg.AuthzHotkey == keyName {
				fmt.Printf("Status: 🔥 ACTIVE HOT KEY\n")
				if cfg.AuthzGranter != "" {
					fmt.Printf("Operator: %s\n", cfg.AuthzGranter)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&addressOnly, "address", false, "Show only the address")
	cmd.Flags().BoolVar(&pubkeyOnly, "pubkey", false, "Show only the public key")

	return cmd
}

// keysDeleteCmd deletes a key from the keyring
func keysDeleteCmd() *cobra.Command {
	var skipConfirmation bool

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a key from keyring",
		Long: `
Delete a key from the keyring. This action cannot be undone.
Make sure you have backed up the key before deletion.
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyName := args[0]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use command line flag if provided, otherwise use config
			backend := cfg.KeyringBackend
			if keyringBackendFlag != "file" && keyringBackendFlag != "" {
				backend = config.KeyringBackend(keyringBackendFlag)
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Check if key exists
			record, err := kb.Key(keyName)
			if err != nil {
				return fmt.Errorf("key '%s' not found: %w", keyName, err)
			}

			// Get address for display
			addr, _ := record.GetAddress()

			// Warn if this is the active hot key
			if cfg.AuthzHotkey == keyName {
				fmt.Printf("⚠️  WARNING: This is your ACTIVE HOT KEY!\n")
				fmt.Printf("Deleting this key will disable Universal Validator operations.\n\n")
			}

			if !skipConfirmation {
				fmt.Printf("Are you sure you want to delete key '%s' (%s)? [y/N]: ", keyName, addr.String())
				reader := bufio.NewReader(os.Stdin)
				response, err := reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read confirmation: %w", err)
				}

				response = strings.TrimSpace(strings.ToLower(response))
				if response != "y" && response != "yes" {
					fmt.Println("Deletion cancelled")
					return nil
				}
			}

			// Delete the key
			err = keys.DeleteKey(kb, keyName)
			if err != nil {
				return fmt.Errorf("failed to delete key: %w", err)
			}

			fmt.Printf("✅ Key '%s' deleted successfully\n", keyName)

			// Warn about config update if this was the active hot key
			if cfg.AuthzHotkey == keyName {
				fmt.Printf("\n⚠️  Remember to update your config file to remove or change the authz_hotkey setting.\n")
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&skipConfirmation, "yes", false, "Skip confirmation prompt")

	return cmd
}

// keysExportCmd exports a key for backup
func keysExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <name>",
		Short: "Export a key for backup",
		Long: `
Export a key in encrypted format for backup purposes.
You will need to provide the key's passphrase to export it.
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyName := args[0]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use command line flag if provided, otherwise use config
			backend := cfg.KeyringBackend
			if keyringBackendFlag != "file" && keyringBackendFlag != "" {
				backend = config.KeyringBackend(keyringBackendFlag)
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Check if key exists
			if err := keys.ValidateKeyExists(kb, keyName); err != nil {
				return fmt.Errorf("key '%s' not found: %w", keyName, err)
			}

			// Get passphrase if file backend
			var passphrase string
			if backend == config.KeyringBackendFile {
				passphrase, err = getPassphrase("Enter passphrase for key: ", false)
				if err != nil {
					return fmt.Errorf("failed to get passphrase: %w", err)
				}
			}

			// Export key
			armor, err := keys.ExportKey(kb, keyName, passphrase)
			if err != nil {
				return fmt.Errorf("failed to export key: %w", err)
			}

			fmt.Printf("Exported key '%s':\n\n", keyName)
			fmt.Println(armor)
			fmt.Printf("\n⚠️  Keep this backup safe and secure!\n")

			return nil
		},
	}

	return cmd
}

// keysImportCmd imports a key from backup
func keysImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import <name> [file]",
		Short: "Import a key from backup",
		Long: `
Import a key from encrypted backup format.
Provide a file path to read from file, or omit to enter key data interactively.

Examples:
  puniversald keys import my-key backup.json
  puniversald keys import my-key < backup.json
  puniversald keys import my-key    # Interactive input
`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			keyName := args[0]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Use command line flag if provided, otherwise use config
			backend := cfg.KeyringBackend
			if keyringBackendFlag != "file" && keyringBackendFlag != "" {
				backend = config.KeyringBackend(keyringBackendFlag)
			}

			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, backend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Check if key already exists
			if err := keys.ValidateKeyExists(kb, keyName); err == nil {
				return fmt.Errorf("key with name '%s' already exists", keyName)
			}

			// Get encrypted key data
			var armor string
			if len(args) > 1 {
				// Read from file
				filePath := args[1]
				fileData, err := os.ReadFile(filePath)
				if err != nil {
					return fmt.Errorf("failed to read file '%s': %w", filePath, err)
				}
				armor = strings.TrimSpace(string(fileData))
				fmt.Printf("Reading key data from file: %s\n", filePath)
			} else {
				// Read from stdin
				fmt.Print("Enter the encrypted key data (paste and press Enter):\n")
				reader := bufio.NewReader(os.Stdin)
				armor, err = reader.ReadString('\n')
				if err != nil {
					return fmt.Errorf("failed to read key data: %w", err)
				}
				armor = strings.TrimSpace(armor)
			}

			// Get passphrase
			var passphrase string
			if backend == config.KeyringBackendFile {
				passphrase, err = getPassphrase("Enter passphrase to decrypt key: ", false)
				if err != nil {
					return fmt.Errorf("failed to get passphrase: %w", err)
				}
			}

			// Import key
			err = keys.ImportKey(kb, keyName, armor, passphrase)
			if err != nil {
				return fmt.Errorf("failed to import key: %w", err)
			}

			// Get address for confirmation
			record, err := kb.Key(keyName)
			if err != nil {
				return fmt.Errorf("failed to verify imported key: %w", err)
			}

			addr, err := record.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get address: %w", err)
			}

			fmt.Printf("✅ Key '%s' imported successfully!\n", keyName)
			fmt.Printf("Address: %s\n", addr.String())

			return nil
		},
	}

	return cmd
}

// Helper function to get passphrase securely
func getPassphrase(prompt string, confirm bool) (string, error) {
	fmt.Print(prompt)
	
	// Read password without echoing
	passBytes, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		return "", err
	}
	fmt.Println() // New line after password input
	
	passphrase := string(passBytes)
	
	if confirm {
		fmt.Print("Confirm passphrase: ")
		confirmBytes, err := term.ReadPassword(int(syscall.Stdin))
		if err != nil {
			return "", err
		}
		fmt.Println()
		
		if passphrase != string(confirmBytes) {
			return "", fmt.Errorf("passphrases do not match")
		}
	}
	
	return passphrase, nil
}

// Helper function to get mnemonic from record (placeholder - would need actual implementation)
func getMnemonicFromRecord(record *keyring.Record, passphrase string) (string, error) {
	// This is a placeholder - actual implementation would depend on how we can extract
	// the mnemonic from the keyring record. This might require additional keyring methods.
	return "word1 word2 word3 ... word24", fmt.Errorf("mnemonic extraction not implemented yet")
}

// getKeybase creates an instance of Keybase for CLI commands
func getKeybase(homeDir string, reader io.Reader, keyringBackend config.KeyringBackend) (keyring.Keyring, error) {
	if len(homeDir) == 0 {
		return nil, fmt.Errorf("home directory is empty")
	}
	
	// Create interface registry and codec with EVM support
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

	// Determine backend type
	var backend string
	switch keyringBackend {
	case config.KeyringBackendFile:
		backend = "file"
	case config.KeyringBackendTest:
		backend = "test"
	default:
		backend = "test" // Default to test backend
	}

	// Create keyring with appropriate backend and EVM compatibility
	return keyring.New(sdk.KeyringServiceName(), backend, homeDir, reader, cdc, cosmosevmkeyring.Option())
}

// keysSecurityCmd shows security status and recommendations
func keysSecurityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "security",
		Short: "Show security status and recommendations",
		Long: `
Show security status and recommendations for hot key management.
This command analyzes the current keyring setup and provides
security recommendations to improve the safety of key operations.

Examples:
  puniversald keys security
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load configuration to get keyring settings
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				log.Warn().Err(err).Msg("Config not found, using defaults")
				cfg = config.Config{}
			}

			// Display security status
			fmt.Println("🔒 Universal Validator Hot Key Security Status")
			fmt.Println("===============================================")
			
			// Check if hot key is configured
			if !config.IsHotKeyConfigured(&cfg) {
				fmt.Println("❌ Hot key management is not configured")
				fmt.Println("\nTo configure hot keys:")
				fmt.Println("1. Run: puniversald init --authz-granter=<operator-address> --authz-hotkey=<key-name>")
				fmt.Println("2. Run: puniversald keys add <key-name>")
				fmt.Println("3. Run: puniversald authz grant")
				return nil
			}

			fmt.Printf("✅ Hot Key Name: %s\n", cfg.AuthzHotkey)
			fmt.Printf("✅ Operator Address: %s\n", cfg.AuthzGranter)
			fmt.Printf("ℹ️  Keyring Backend: %s\n", cfg.KeyringBackend)
			
			// Use the same keyring directory as the EVM key commands
			keyringDir := constant.DefaultNodeHome
			fmt.Printf("ℹ️  Keyring Directory: %s\n", keyringDir)

			// Create security manager for analysis
			securityManager := keys.NewKeySecurityManager(keys.SecurityLevelMedium, keyringDir)

			// Check keyring directory security
			if err := securityManager.ValidateKeyringDirectory(); err != nil {
				fmt.Printf("❌ Keyring Directory Security: %v\n", err)
			} else {
				fmt.Println("✅ Keyring Directory Security: Validated")
			}

			// Check environment security
			secCheck := keys.IsSecureEnvironment()
			if secCheck.IsInteractive {
				fmt.Println("✅ Environment: Interactive terminal detected")
			} else {
				fmt.Println("⚠️  Environment: Non-interactive terminal")
			}

			// Try to validate key if it exists
			if cfg.AuthzHotkey != "" {
				backend := config.KeyringBackend(keyringBackendFlag)
				if keyringBackendFlag == "" {
					backend = cfg.KeyringBackend
				}
				
				// Try to access keyring to check if key exists
				kb, err := getKeybase(keyringDir, nil, backend)
				if err == nil {
					if err := securityManager.ValidateKeyIntegrity(kb, cfg.AuthzHotkey); err != nil {
						fmt.Printf("❌ Hot Key Integrity: %v\n", err)
					} else {
						fmt.Println("✅ Hot Key Integrity: Validated")
						
						// Show key fingerprint for verification
						fingerprint, err := securityManager.GenerateKeyFingerprint(kb, cfg.AuthzHotkey)
						if err == nil {
							fmt.Printf("🔑 Key Fingerprint: %s\n", fingerprint)
						}
					}
				} else {
					fmt.Printf("⚠️  Key Access: Unable to access keyring - %v\n", err)
				}
			}

			// Get and display security recommendations
			recommendations := securityManager.GetSecurityRecommendations()
			keys.PrintSecurityRecommendations(recommendations)

			// Show additional security tips
			fmt.Println("\n💡 Security Best Practices:")
			fmt.Println("=========================")
			fmt.Println("• Use 'file' backend for production (encrypted key storage)")
			fmt.Println("• Use strong passwords (8+ chars, mixed case, numbers, symbols)")
			fmt.Println("• Regularly backup your keyring directory")
			fmt.Println("• Keep your operator key offline and secure")
			fmt.Println("• Monitor key access logs for suspicious activity")
			fmt.Println("• Rotate hot keys periodically for enhanced security")

			return nil
		},
	}

	return cmd
}