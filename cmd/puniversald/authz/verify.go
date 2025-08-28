package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/keys"
)

// VerifyCmd creates the authz verify command
func VerifyCmd(rpcEndpoint, chainID *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Verify hot key has required permissions",
		Long: `
Verify that the configured hot key has all required permissions to operate
as a Universal Validator. This checks the current configuration and validates
that AuthZ grants are properly set up.

Examples:
  puniversald authz verify
  puniversald authz verify --rpc localhost:9090
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runVerifyCommand(*rpcEndpoint, *chainID)
		},
	}

	return cmd
}

func runVerifyCommand(rpcEndpoint, chainID string) error {
	// Load config
	cfg, err := config.Load(constant.DefaultNodeHome)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	// Get allowed message types from config
	allowedTypes := cfg.GetAllowedMessageTypes()

	// Validate hot key configuration
	if err := config.ValidateHotKeyConfig(&cfg); err != nil {
		return fmt.Errorf("hot key configuration is invalid: %w", err)
	}

	fmt.Printf("🔍 Verifying hot key setup...\n")
	fmt.Printf("Operator: %s\n", cfg.AuthzGranter)
	fmt.Printf("Hot Key: %s\n", cfg.AuthzHotkey)
	fmt.Printf("Allowed Message Types: %d configured\n", len(allowedTypes))

	// Parse operator address
	operatorAddr, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
	if err != nil {
		return fmt.Errorf("invalid operator address: %w", err)
	}

	// Get hot key address
	keyringDir := constant.DefaultNodeHome
	kb, err := keys.CreateKeyringFromConfig(keyringDir, nil, cfg.KeyringBackend)
	if err != nil {
		return fmt.Errorf("failed to create keyring: %w", err)
	}

	hotkeyRecord, err := kb.Key(cfg.AuthzHotkey)
	if err != nil {
		return fmt.Errorf("hot key '%s' not found in keyring: %w", cfg.AuthzHotkey, err)
	}

	hotkeyAddr, err := hotkeyRecord.GetAddress()
	if err != nil {
		return fmt.Errorf("failed to get hot key address: %w", err)
	}

	// Create gRPC connection
	conn, err := grpc.Dial(rpcEndpoint, grpc.WithInsecure())
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
	}
	defer conn.Close()

	// Create authz query client
	authzClient := authz.NewQueryClient(conn)
	ctx := context.Background()

	// Check each required grant
	requiredMsgTypes := allowedTypes
	var missingGrants []string
	var validGrants []string

	fmt.Printf("\n📋 Checking required permissions:\n")

	for _, msgType := range requiredMsgTypes {
		// Query specific grant
		grantResp, err := authzClient.Grants(ctx, &authz.QueryGrantsRequest{
			Granter:    operatorAddr.String(),
			Grantee:    hotkeyAddr.String(),
			MsgTypeUrl: msgType,
		})

		if err != nil || len(grantResp.Grants) == 0 {
			missingGrants = append(missingGrants, msgType)
			fmt.Printf("  ❌ %s - MISSING\n", msgType)
		} else {
			grant := grantResp.Grants[0]
			if grant.Expiration != nil && grant.Expiration.Before(time.Now()) {
				missingGrants = append(missingGrants, msgType)
				fmt.Printf("  ⏰ %s - EXPIRED\n", msgType)
			} else {
				validGrants = append(validGrants, msgType)
				fmt.Printf("  ✅ %s - OK", msgType)
				if grant.Expiration != nil {
					fmt.Printf(" (expires: %s)", grant.Expiration.Format("2006-01-02"))
				}
				fmt.Println()
			}
		}
	}

	// Summary
	fmt.Printf("\n📊 Summary:\n")
	fmt.Printf("Configured message types: %d\n", len(allowedTypes))
	fmt.Printf("Valid grants: %d/%d\n", len(validGrants), len(requiredMsgTypes))

	if len(missingGrants) > 0 {
		fmt.Printf("Missing grants: %d\n", len(missingGrants))
		fmt.Printf("\n⚠️  Hot key setup is INCOMPLETE\n")
		fmt.Printf("The following message types require grants:\n")
		for _, msgType := range missingGrants {
			fmt.Printf("  - %s\n", msgType)
		}
		fmt.Printf("\nRun the following command to fix:\n")
		fmt.Printf("  puniversald authz grant <operator-key> %s\n", cfg.AuthzHotkey)
		return fmt.Errorf("hot key verification failed")
	}

	fmt.Printf("\n🎉 Hot key setup is COMPLETE and ready for transaction execution!\n")

	return nil
}