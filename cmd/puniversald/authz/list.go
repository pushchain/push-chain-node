package authz

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cosmos/cosmos-sdk/x/authz"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/keys"
)

// ListCmd creates the authz list command
func ListCmd(rpcEndpoint, chainID *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <account>",
		Short: "List all grants for an account",
		Long: `
List all authorization grants where the specified account is either granter or grantee.
This helps verify what permissions have been granted.

Examples:
  puniversald authz list push1abc...
  puniversald authz list my-validator-key
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListCommand(args[0], *rpcEndpoint)
		},
	}

	return cmd
}

func runListCommand(account, rpcEndpoint string) error {
	// Load config
	cfg, err := config.Load(constant.DefaultNodeHome)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Use the same keyring directory as the EVM key commands
	keyringDir := constant.DefaultNodeHome

	// Create keyring
	kb, err := keys.CreateKeyringFromConfig(keyringDir, nil, cfg.KeyringBackend)
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}

	// Resolve account to address
	accountAddr, err := resolveAccountAddress(account, kb)
	if err != nil {
		return err
	}

	// Ensure endpoint has gRPC port
	grpcEndpoint := ensureGRPCPort(rpcEndpoint)

	// Create gRPC connection
	conn, err := grpc.NewClient(grpcEndpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("Warning: failed to close gRPC connection: %v\n", err)
		}
	}()

	// Create authz query client
	authzClient := authz.NewQueryClient(conn)

	ctx := context.Background()

	// Query grants by granter
	fmt.Printf("Grants where %s is the GRANTER:\n", accountAddr)
	granterResp, err := authzClient.GranterGrants(ctx, &authz.QueryGranterGrantsRequest{
		Granter: accountAddr.String(),
	})
	if err == nil && len(granterResp.Grants) > 0 {
		for _, grant := range granterResp.Grants {
			fmt.Printf("  → To: %s\n", grant.Grantee)
			fmt.Printf("    Authorization: %s\n", grant.Authorization.TypeUrl)
			if grant.Expiration != nil {
				fmt.Printf("    Expires: %s\n", grant.Expiration.String())
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("  No grants found\n\n")
	}

	// Query grants by grantee
	fmt.Printf("Grants where %s is the GRANTEE:\n", accountAddr)
	granteeResp, err := authzClient.GranteeGrants(ctx, &authz.QueryGranteeGrantsRequest{
		Grantee: accountAddr.String(),
	})
	if err == nil && len(granteeResp.Grants) > 0 {
		for _, grant := range granteeResp.Grants {
			fmt.Printf("  → From: %s\n", grant.Granter)
			fmt.Printf("    Authorization: %s\n", grant.Authorization.TypeUrl)
			if grant.Expiration != nil {
				fmt.Printf("    Expires: %s\n", grant.Expiration.String())
			}
			fmt.Println()
		}
	} else {
		fmt.Printf("  No grants found\n\n")
	}

	return nil
}