package main

import (
	"github.com/spf13/cobra"

	authzcmd "github.com/rollchains/pchain/cmd/puniversald/authz"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
)

var (
	nodeEndpoint string
	chainID      string
)

// authzCmd returns the authz command with all subcommands
func authzCmd() *cobra.Command {
	// Load config to get default endpoint
	cfg, err := config.Load(constant.DefaultNodeHome)
	defaultEndpoint := "localhost"
	if err == nil && len(cfg.PushChainGRPCURLs) > 0 {
		// Use first configured URL as default (should be clean base URL without port)
		defaultEndpoint = cfg.PushChainGRPCURLs[0]
	}

	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Use AuthZ grants for Universal Validator hot keys",
		Long: `
The authz commands allow you to use authorization grants for Universal Validator operations.
Hot keys can execute transactions on behalf of operator accounts using granted permissions.

Note: Grant creation and revocation are handled by the core validator (pchaind).
Use the local-validator-manager setup-container-authz command to create grants.

Available Commands:
  list     List all grants for an account
  verify   Verify hot key has required permissions
  exec     Execute a transaction using AuthZ grants
`,
	}

	// Add persistent flags
	cmd.PersistentFlags().StringVar(&nodeEndpoint, "node", defaultEndpoint, "Base URL for Push Chain node (gRPC: :9090, RPC: :26657)")
	cmd.PersistentFlags().StringVar(&chainID, "chain-id", "pchain", "Chain ID for transactions")

	// Add subcommands - only operations that universal validator should perform
	cmd.AddCommand(authzcmd.ListCmd(&nodeEndpoint, &chainID))
	cmd.AddCommand(authzcmd.VerifyCmd(&nodeEndpoint, &chainID))
	cmd.AddCommand(authzcmd.ExecCmd(&nodeEndpoint, &chainID))

	return cmd
}