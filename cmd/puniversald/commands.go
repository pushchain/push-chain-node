package main

import (
	"context"
	"fmt"
	"path/filepath"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/core"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/rollchains/pchain/universalClient/store"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var cfg config.Config

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(setblockCmd())
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
	var cfg config.Config

	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create initial config file via flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Save config
			if err := config.Save(&cfg, constant.DefaultNodeHome); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			fmt.Printf("✅ Config saved to %s/config/pushuv_config.json\n", constant.DefaultNodeHome)
			return nil
		},
	}

	// Bind flags to cfg fields
	cmd.Flags().IntVar(&cfg.LogLevel, "log-level", 1, "Log level (0=debug, 1=info, ..., 5=panic)")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", "console", "Log format: json or console")
	cmd.Flags().BoolVar(&cfg.LogSampler, "log-sampler", false, "Enable log sampling")

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
		chainID string
		block   int64
		list    bool
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
					var blocks []store.LastObservedBlock
					if err := chainDB.Client().Where("chain_id = ?", chainID).Find(&blocks).Error; err != nil {
						fmt.Printf("Error reading chain %s: %v\n", chainID, err)
						continue
					}
					
					for _, block := range blocks {
						fmt.Printf("Chain: %s\n", block.ChainID)
						fmt.Printf("Block: %d\n", block.Block)
						fmt.Printf("Updated: %v\n", block.UpdatedAt)
						fmt.Printf("Database: %s\n", chainID)
						fmt.Println("--------------------------------")
					}
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

			var lastBlock store.LastObservedBlock
			result := database.Client().Where("chain_id = ?", chainID).First(&lastBlock)
			
			if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
				return fmt.Errorf("failed to query last observed block: %w", result.Error)
			}
			
			if result.Error == gorm.ErrRecordNotFound {
				// Create new record
				lastBlock = store.LastObservedBlock{
					ChainID: chainID,
					Block:   block,
				}
				if err := database.Client().Create(&lastBlock).Error; err != nil {
					return fmt.Errorf("failed to create last observed block record: %w", err)
				}
				fmt.Printf("Created new record for chain %s at block %d\n", chainID, block)
			} else {
				// Update existing record
				oldBlock := lastBlock.Block
				lastBlock.Block = block
				if err := database.Client().Save(&lastBlock).Error; err != nil {
					return fmt.Errorf("failed to update last observed block: %w", err)
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
