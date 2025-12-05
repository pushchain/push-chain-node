package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/core"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/logger"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/spf13/cobra"

	cosmosevmcmd "github.com/cosmos/evm/client"
	"gorm.io/gorm"
)

var cfg config.Config

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(cosmosevmcmd.KeyCommands(constant.DefaultNodeHome, true))
	rootCmd.AddCommand(authzCmd())
	rootCmd.AddCommand(setblockCmd())
	rootCmd.AddCommand(tssPeerIDCmd())
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print universal validator version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Name:       %s\n", sdkversion.Name)
			fmt.Printf("App Name:   %s\n", sdkversion.AppName)
			fmt.Printf("Version:    %s\n", sdkversion.Version)
			fmt.Printf("Commit:     %s\n", sdkversion.Commit)
			fmt.Printf("Build Tags: %s\n", sdkversion.BuildTags)
		},
	}
}

func initCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create initial config file with default values",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load default config
			cfg, err := config.LoadDefaultConfig()
			if err != nil {
				return fmt.Errorf("failed to load default config: %w", err)
			}

			// Override with flags if provided
			if cmd.Flags().Changed("log-level") {
				logLevel, _ := cmd.Flags().GetInt("log-level")
				cfg.LogLevel = logLevel
			}
			if cmd.Flags().Changed("log-format") {
				logFormat, _ := cmd.Flags().GetString("log-format")
				cfg.LogFormat = logFormat
			}
			if cmd.Flags().Changed("log-sampler") {
				logSampler, _ := cmd.Flags().GetBool("log-sampler")
				cfg.LogSampler = logSampler
			}

			// Save config
			if err := config.Save(&cfg, constant.DefaultNodeHome); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			fmt.Printf("✅ Config saved to %s/config/pushuv_config.json\n", constant.DefaultNodeHome)
			return nil
		},
	}

	// Define flags (not bound to a specific cfg instance)
	cmd.Flags().Int("log-level", 1, "Log level (0=debug, 1=info, ..., 5=panic)")
	cmd.Flags().String("log-format", "console", "Log format: json or console")
	cmd.Flags().Bool("log-sampler", false, "Enable log sampling")

	return cmd
}

func startCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the universal message handler",
		RunE: func(cmd *cobra.Command, args []string) error {
			// --- Step 1: Load config ---
			loadedCfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Print loaded config as JSON
			configJSON, err := json.MarshalIndent(loadedCfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			fmt.Printf("\n=== Loaded Configuration ===\n%s\n===========================\n\n", string(configJSON))

			// --- Step 2: Setup logger ---
			log := logger.Init(loadedCfg)

			// --- Step 3: Setup ChainDBManager ---
			// Set default database base directory if not configured
			if loadedCfg.DatabaseBaseDir == "" {
				loadedCfg.DatabaseBaseDir = filepath.Join(constant.DefaultNodeHome, "databases")
			}

			dbManager := db.NewChainDBManager(loadedCfg.DatabaseBaseDir, log, &loadedCfg)

			// --- Step 4: Start client ---
			ctx := context.Background()
			client, err := core.NewUniversalClient(ctx, log, dbManager, &loadedCfg)
			if err != nil {
				return fmt.Errorf("failed to create universal client: %w", err)
			}
			return client.Start()
		},
	}
	return cmd
}

func setblockCmd() *cobra.Command {
	var (
		chainID  string
		block    int64
		list     bool
		blockSet bool
	)

	cmd := &cobra.Command{
		Use:   "setblock",
		Short: "Set or list last observed blocks for chains",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config to get database base directory
			loadedCfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Set default database base directory if not configured
			if loadedCfg.DatabaseBaseDir == "" {
				loadedCfg.DatabaseBaseDir = filepath.Join(constant.DefaultNodeHome, "databases")
			}

			// Setup logger (minimal for CLI)
			log := logger.Init(loadedCfg)

			// Create ChainDBManager
			dbManager := db.NewChainDBManager(loadedCfg.DatabaseBaseDir, log, &loadedCfg)
			defer dbManager.CloseAll()

			// List mode
			if list {
				databases := dbManager.GetAllDatabases()
				if len(databases) == 0 {
					fmt.Println("No chain databases found")
					return nil
				}

				fmt.Println("\nCurrent last observed blocks:")
				fmt.Println("================================")

				for chainID, chainDB := range databases {
					var chainState store.ChainState
					if err := chainDB.Client().First(&chainState).Error; err != nil {
						if err == gorm.ErrRecordNotFound {
							fmt.Printf("No state found for chain %s\n", chainID)
						} else {
							fmt.Printf("Error reading chain %s: %v\n", chainID, err)
						}
						continue
					}

					fmt.Printf("Chain: %s\n", chainID)
					fmt.Printf("Last Block: %d\n", chainState.LastBlock)
					fmt.Printf("Updated: %v\n", chainState.UpdatedAt)
					fmt.Println("--------------------------------")
				}
				return nil
			}

			// Check if block flag was actually provided
			blockSet = cmd.Flags().Changed("block")

			// Set mode
			if chainID == "" || !blockSet {
				return fmt.Errorf("--chain and --block are required when not using --list")
			}

			// Get chain-specific database
			database, err := dbManager.GetChainDB(chainID)
			if err != nil {
				return fmt.Errorf("failed to get database for chain %s: %w", chainID, err)
			}

			var chainState store.ChainState
			result := database.Client().First(&chainState)

			if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
				return fmt.Errorf("failed to query chain state: %w", result.Error)
			}

			if result.Error == gorm.ErrRecordNotFound {
				// Create new record
				chainState = store.ChainState{
					LastBlock: uint64(block),
				}
				if err := database.Client().Create(&chainState).Error; err != nil {
					return fmt.Errorf("failed to create chain state record: %w", err)
				}
				fmt.Printf("Created new record for chain %s at block %d\n", chainID, block)
			} else {
				// Update existing record
				oldBlock := chainState.LastBlock
				chainState.LastBlock = uint64(block)
				if err := database.Client().Save(&chainState).Error; err != nil {
					return fmt.Errorf("failed to update chain state: %w", err)
				}
				fmt.Printf("Updated block from %d to %d for chain %s\n", oldBlock, block, chainID)
			}

			fmt.Printf("✅ Successfully set block %d for chain %s\n", block, chainID)
			return nil
		},
	}

	cmd.Flags().StringVar(&chainID, "chain", "", "Chain ID (e.g., 'eip155:11155111' or 'solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1')")
	cmd.Flags().Int64Var(&block, "block", -1, "Block number to set")
	cmd.Flags().BoolVar(&list, "list", false, "List all current block records")

	return cmd
}

// tssPeerIDCmd computes and prints the libp2p peer ID from a TSS private key hex string.
// This is used during devnet setup to derive the peer ID for universal validator registration.
func tssPeerIDCmd() *cobra.Command {
	var privateKeyHex string

	cmd := &cobra.Command{
		Use:   "tss-peer-id",
		Short: "Compute libp2p peer ID from TSS private key hex",
		Long: `Compute the libp2p peer ID from a 32-byte hex-encoded Ed25519 seed.

This is used during devnet setup to derive the peer ID that matches
what the TSS node will use, for universal validator registration.

Example:
  puniversald tss-peer-id --private-key 0101010101010101010101010101010101010101010101010101010101010101`,
		RunE: func(cmd *cobra.Command, args []string) error {
			privateKeyHex = strings.TrimSpace(privateKeyHex)

			// Decode hex to bytes
			keyBytes, err := hex.DecodeString(privateKeyHex)
			if err != nil {
				return fmt.Errorf("invalid hex: %w", err)
			}
			if len(keyBytes) != 32 {
				return fmt.Errorf("expected 32 bytes, got %d", len(keyBytes))
			}

			// Create Ed25519 key from seed
			privKey := ed25519.NewKeyFromSeed(keyBytes)
			pubKey := privKey.Public().(ed25519.PublicKey)

			// Convert to libp2p format (64 bytes: 32 priv seed + 32 pub)
			libp2pKeyBytes := make([]byte, 64)
			copy(libp2pKeyBytes[:32], privKey[:32])
			copy(libp2pKeyBytes[32:], pubKey)

			libp2pPrivKey, err := crypto.UnmarshalEd25519PrivateKey(libp2pKeyBytes)
			if err != nil {
				return fmt.Errorf("failed to unmarshal Ed25519 key: %w", err)
			}

			// Get peer ID from public key
			peerID, err := peer.IDFromPrivateKey(libp2pPrivKey)
			if err != nil {
				return fmt.Errorf("failed to derive peer ID: %w", err)
			}

			fmt.Println(peerID.String())
			return nil
		},
	}

	cmd.Flags().StringVar(&privateKeyHex, "private-key", "", "Hex-encoded 32-byte Ed25519 seed")
	cmd.MarkFlagRequired("private-key")

	return cmd
}
