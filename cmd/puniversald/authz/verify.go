package authz

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"

	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/keys"
)

// Color constants for terminal output
var (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
)

func init() {
	// Check if colors should be disabled (for CI/CD or non-TTY environments)
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		colorReset = ""
		colorRed = ""
		colorGreen = ""
		colorYellow = ""
	}
}

// Status formatting helpers
func statusOK(msg string) string {
	return fmt.Sprintf("%s[OK]%s   %s", colorGreen, colorReset, msg)
}

func statusFail(msg string) string {
	return fmt.Sprintf("%s[FAIL]%s %s", colorRed, colorReset, msg)
}

func statusExp(msg string) string {
	return fmt.Sprintf("%s[EXP]%s  %s", colorYellow, colorReset, msg)
}


// VerifyCmd creates the authz verify command
func VerifyCmd(rpcEndpoint, chainID *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify <grantee-key> <granter-addr> [msg-types...]",
		Short: "Verify hot key has required permissions",
		Args:  cobra.MinimumNArgs(2),
		Long: `
Verify that the specified hot key has all required permissions to execute
transactions on behalf of the granter. This validates that AuthZ grants
are properly set up for the specified message types.

If no message types are specified, checks default message type:
  /ue.v1.MsgVoteInbound

Examples:
  puniversald authz verify container-hotkey push1granter...
  puniversald authz verify container-hotkey push1granter... /ue.v1.MsgVoteInbound
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerifyCommand(*rpcEndpoint, args)
		},
	}

	return cmd
}

func runVerifyCommand(rpcEndpoint string, args []string) error {
	// Parse arguments
	granteeKeyName := args[0]
	granterAddr := args[1]
	
	// Get message types (use defaults if not provided)
	var msgTypes []string
	if len(args) > 2 {
		msgTypes = args[2:]
	} else {
		// Use default message type for universal validator voting
		msgTypes = []string{
			"/ue.v1.MsgVoteInbound",
		}
	}

	fmt.Printf("Verifying AuthZ configuration...\n")
	fmt.Printf("  Granter:  %s\n", granterAddr)
	fmt.Printf("  Grantee:  %s\n", granteeKeyName)
	fmt.Printf("  Required: %d message types\n", len(msgTypes))

	// Parse granter address
	granterAddress, err := sdk.AccAddressFromBech32(granterAddr)
	if err != nil {
		return fmt.Errorf("invalid granter address: %w", err)
	}

	// Load config for keyring backend
	cfg, err := config.Load(constant.DefaultNodeHome)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Get grantee key address
	keyringDir := constant.DefaultNodeHome
	kb, err := keys.CreateKeyringFromConfig(keyringDir, nil, cfg.KeyringBackend)
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}

	granteeRecord, err := kb.Key(granteeKeyName)
	if err != nil {
		return fmt.Errorf("grantee key '%s' not found in keyring: %w", granteeKeyName, err)
	}

	granteeAddr, err := granteeRecord.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get grantee key address: %w", err)
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

	// Check each required grant
	var missingGrants []string
	var validGrants []string

	fmt.Printf("\nChecking permissions:\n")

	for _, msgType := range msgTypes {
		// Query specific grant
		grantResp, err := authzClient.Grants(ctx, &authz.QueryGrantsRequest{
			Granter:    granterAddress.String(),
			Grantee:    granteeAddr.String(),
			MsgTypeUrl: msgType,
		})

		if err != nil || len(grantResp.Grants) == 0 {
			missingGrants = append(missingGrants, msgType)
			fmt.Printf("  %s\n", statusFail(msgType))
		} else {
			grant := grantResp.Grants[0]
			if grant.Expiration != nil && grant.Expiration.Before(time.Now()) {
				missingGrants = append(missingGrants, msgType)
				fmt.Printf("  %s\n", statusExp(msgType))
			} else {
				validGrants = append(validGrants, msgType)
				msg := msgType
				if grant.Expiration != nil {
					msg += fmt.Sprintf(" (exp: %s)", grant.Expiration.Format("2006-01-02"))
				}
				fmt.Printf("  %s\n", statusOK(msg))
			}
		}
	}

	// Summary
	fmt.Printf("\nStatus: ")
	if len(missingGrants) > 0 {
		fmt.Printf("%sINCOMPLETE%s (%d/%d grants valid)\n", colorRed, colorReset, len(validGrants), len(msgTypes))
		
		fmt.Printf("\nMissing grants:\n")
		for _, msgType := range missingGrants {
			fmt.Printf("  %s\n", msgType)
		}
		fmt.Printf("\nTo grant missing permissions, use the core validator (pchaind) or local-validator-manager setup-container-authz\n")
		return fmt.Errorf("verification failed - missing grants")
	}

	fmt.Printf("%sREADY%s (all grants valid)\n", colorGreen, colorReset)

	return nil
}