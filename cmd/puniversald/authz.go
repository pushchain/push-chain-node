package main

import (
	"github.com/spf13/cobra"

	authzcmd "github.com/rollchains/pchain/cmd/puniversald/authz"
)

var (
	rpcEndpoint string
	chainID     string
)

// authzCmd returns the authz command with all subcommands
func authzCmd() *cobra.Command {
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
	cmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc", "localhost:9090", "gRPC endpoint for Push Chain")
	cmd.PersistentFlags().StringVar(&chainID, "chain-id", "pchain", "Chain ID for transactions")

	// Add subcommands - only operations that universal validator should perform
	cmd.AddCommand(authzcmd.ListCmd(&rpcEndpoint, &chainID))
	cmd.AddCommand(authzcmd.VerifyCmd(&rpcEndpoint, &chainID))
	cmd.AddCommand(authzcmd.ExecCmd(&rpcEndpoint, &chainID))

	return cmd
}