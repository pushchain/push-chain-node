package main

import (
	"fmt"
	"os"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file if available
	_ = godotenv.Load()

	// Setup custom Bech32 prefixes and other Cosmos SDK config
	setupSDKConfig()

	// Construct root command
	rootCmd := NewRootCmd()

	// Execute CLI
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(rootCmd.OutOrStderr(), err)
		os.Exit(1)
	}
}

func setupSDKConfig() {
	config := sdk.GetConfig()

	// Push Chain prefixes
	config.SetBech32PrefixForAccount("push", "pushpub")
	config.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	config.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
	config.SetCoinType(60) // Ethereum coin type

	config.Seal()
}
