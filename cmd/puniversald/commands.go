package main

import (
	"context"
	"fmt"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/core"
	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/spf13/cobra"
)

var cfg config.Config

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(startCmd())
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
			if err := config.Save(&cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			fmt.Printf("✅ Config saved to %s/config/pushuv_config.json\n", cfg.NodeDir)
			return nil
		},
	}

	// Bind flags to cfg fields
	cmd.Flags().StringVar(&cfg.NodeDir, "node-dir", ".pushuniversal", "Directory to store config and DB")
	cmd.Flags().IntVar(&cfg.LogLevel, "log-level", 1, "Log level (0=debug, 1=info, ..., 5=panic)")
	cmd.Flags().StringVar(&cfg.LogFormat, "log-format", "console", "Log format: json or console")
	cmd.Flags().BoolVar(&cfg.LogSampler, "log-sampler", false, "Enable log sampling")

	return cmd
}

func startCmd() *cobra.Command {
	var nodeDir string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the universal message handler",
		RunE: func(cmd *cobra.Command, args []string) error {
			if nodeDir == "" {
				nodeDir = ".pushuniversal" // default fallback
			}

			// --- Step 1: Load config ---
			loadedCfg, err := config.Load(nodeDir)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// --- Step 2: Setup logger ---
			log := logger.Init(loadedCfg)

			// --- Step 3: Setup DB ---
			database, err := db.OpenFileDB(loadedCfg.NodeDir, "pushuv.db", true)
			if err != nil {
				return fmt.Errorf("failed to load db: %w", err)
			}

			// --- Step 4: Start client ---
			ctx := context.Background()
			client := core.NewUniversalClient(ctx, log, database)
			return client.Start()
		},
	}

	cmd.Flags().StringVar(&nodeDir, "node-dir", ".pushuniversal", "Directory where config and DB are stored")

	return cmd
}
