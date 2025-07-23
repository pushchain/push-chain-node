package main

import (
	"context"
	"fmt"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/rollchains/pchain/universalClient/core"
	"github.com/rollchains/pchain/universalClient/logger"
	"github.com/spf13/cobra"
)

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(versionCmd())
}

func startCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the universal message handler",
		RunE: func(cmd *cobra.Command, args []string) error {
			log := logger.New("info")
			ctx := context.Background()
			client := core.NewUniversalClient(ctx, log)
			return client.Start()
		},
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print puniversald version info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("Name:       %s\n", sdkversion.Name)
			fmt.Printf("App Name:   %s\n", sdkversion.AppName)
			fmt.Printf("Version:    %s\n", sdkversion.Version)
			fmt.Printf("Commit:     %s\n", sdkversion.Commit)
			fmt.Printf("Build Tags: %s\n", sdkversion.BuildTags)
		},
	}
}
