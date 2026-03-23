package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	cosmosevmcmd "github.com/cosmos/evm/client"
	uvconfig "github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/core"
	"github.com/spf13/cobra"
)

const flagHome = "home"

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.PersistentFlags().String(flagHome, uvconfig.DefaultNodeHome(), "node home directory")

	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(cosmosevmcmd.KeyCommands(uvconfig.DefaultNodeHome(), true))
}

// getHome reads the --home flag, falling back to DefaultNodeHome.
func getHome(cmd *cobra.Command) string {
	home, _ := cmd.Flags().GetString(flagHome)
	if home == "" {
		home = uvconfig.DefaultNodeHome()
	}
	return home
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print universal validator version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Name:       puniversald\n")
			fmt.Printf("Version:    %s\n", sdkversion.Version)
			fmt.Printf("Commit:     %s\n", sdkversion.Commit)
			fmt.Printf("Build Tags: %s\n", sdkversion.BuildTags)
		},
	}
}

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Initialize configuration file",
		Long: `Initialize the configuration file with default values.

By default creates the config at ~/.puniversal/config/pushuv_config.json.
Use --home to specify a different directory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			home := getHome(cmd)

			defaultCfg, err := uvconfig.LoadDefaultConfig()
			if err != nil {
				return fmt.Errorf("failed to load default config: %w", err)
			}

			if err := uvconfig.Save(&defaultCfg, home); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			configPath := filepath.Join(home, uvconfig.ConfigSubdir, uvconfig.ConfigFileName)
			fmt.Printf("Configuration file initialized at: %s\n", configPath)
			return nil
		},
	}
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the universal validator",
		RunE: func(cmd *cobra.Command, args []string) error {
			home := getHome(cmd)

			loadedCfg, err := uvconfig.Load(home)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			configJSON, err := json.MarshalIndent(loadedCfg, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal config: %w", err)
			}
			fmt.Printf("\n=== Loaded Configuration ===\n%s\n===========================\n\n", string(configJSON))

			ctx := context.Background()
			client, err := core.NewUniversalClient(ctx, &loadedCfg)
			if err != nil {
				return fmt.Errorf("failed to create universal client: %w", err)
			}
			return client.Start()
		},
	}
}

