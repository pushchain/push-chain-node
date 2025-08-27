package main

import (
	"context"
	"fmt"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
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
	rootCmd.AddCommand(queryCmd())
	rootCmd.AddCommand(keysCmd())
	rootCmd.AddCommand(authzCmd())
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

			// --- Step 3: Setup DB ---
			// TODO: Change db setup - Each chain will have its own DB
			database, err := db.OpenFileDB(constant.DefaultNodeHome+"/data", "pushuv.db", true)
			if err != nil {
				return fmt.Errorf("failed to load db: %w", err)
			}

			// --- Step 4: Start client ---
			ctx := context.Background()
			client, err := core.NewUniversalClient(ctx, log, database, &loadedCfg)
			if err != nil {
				return fmt.Errorf("failed to create universal client: %w", err)
			}
			return client.Start()
		},
	}
	return cmd
}
