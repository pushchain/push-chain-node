package main

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	sdkversion "github.com/cosmos/cosmos-sdk/version"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/core"
	"github.com/spf13/cobra"

	cosmosevmcmd "github.com/cosmos/evm/client"
)

func InitRootCmd(rootCmd *cobra.Command) {
	rootCmd.AddCommand(versionCmd())
	rootCmd.AddCommand(startCmd())
	rootCmd.AddCommand(initCmd())
	rootCmd.AddCommand(cosmosevmcmd.KeyCommands(constant.DefaultNodeHome, true))
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
		Short: "Initialize configuration file",
		Long: `Initialize the configuration file with default values.

This command creates a default configuration file at:
  ~/.puniversal/config/pushuv_config.json

You can edit this file to customize your universal validator settings.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load default config
			defaultCfg, err := config.LoadDefaultConfig()
			if err != nil {
				return fmt.Errorf("failed to load default config: %w", err)
			}

			// Save to config directory
			if err := config.Save(&defaultCfg, constant.DefaultNodeHome); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}

			configPath := fmt.Sprintf("%s/%s/%s", constant.DefaultNodeHome, constant.ConfigSubdir, constant.ConfigFileName)
			fmt.Printf("✅ Configuration file initialized at: %s\n", configPath)
			fmt.Println("You can now edit this file to customize your settings.")
			return nil
		},
	}
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

			// --- Step 2: Start client ---
			ctx := context.Background()
			client, err := core.NewUniversalClient(ctx, &loadedCfg)
			if err != nil {
				return fmt.Errorf("failed to create universal client: %w", err)
			}
			return client.Start()
		},
	}
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
