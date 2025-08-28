package authz

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"

	uauthz "github.com/rollchains/pchain/universalClient/authz"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
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
	
	// Apply the message type configuration to the authz module
	category, customTypes, err := config.GetAuthZMessageTypeConfig(&cfg)
	if err != nil {
		return fmt.Errorf("failed to get authz message type config: %w", err)
	}
	
	switch category {
	case "universal-validator":
		uauthz.UseUniversalValidatorMsgTypes()
	case "custom":
		uauthz.SetAllowedMsgTypes(customTypes)
	case "default":
		uauthz.UseDefaultMsgTypes()
	}

	// Validate hot key configuration
	if err := config.ValidateHotKeyConfig(&cfg); err != nil {
		return fmt.Errorf("hot key configuration is invalid: %w", err)
	}

	fmt.Printf("🔍 Verifying hot key setup...\n")
	fmt.Printf("Operator: %s\n", cfg.AuthzGranter)
	fmt.Printf("Hot Key: %s\n", cfg.AuthzHotkey)
	fmt.Printf("Message Type Category: %s\n", uauthz.GetMsgTypeCategory())

	// Parse operator address
	operatorAddr, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
	if err != nil {
		return fmt.Errorf("invalid operator address: %w", err)
	}

	// Get hot key address
	keyringDir := constant.DefaultNodeHome
	kb, err := setupKeyring(keyringDir, nil, cfg.KeyringBackend)
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
	requiredMsgTypes := uauthz.GetAllAllowedMsgTypes()
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
	fmt.Printf("Message Type Category: %s\n", uauthz.GetMsgTypeCategory())
	fmt.Printf("Valid grants: %d/%d\n", len(validGrants), len(requiredMsgTypes))

	if len(missingGrants) > 0 {
		fmt.Printf("Missing grants: %d\n", len(missingGrants))
		fmt.Printf("\n⚠️  Hot key setup is INCOMPLETE\n")
		
		category := uauthz.GetMsgTypeCategory()
		switch category {
		case "default":
			fmt.Printf("Currently checking for standard Cosmos SDK message types.\n")
			fmt.Printf("These are suitable for testing and basic operations.\n")
		case "universal-validator":
			fmt.Printf("Currently checking for Universal Validator specific message types.\n")
			fmt.Printf("These require Universal Validator modules in the chain.\n")
		case "custom":
			fmt.Printf("Currently checking for custom configured message types.\n")
		}
		
		fmt.Printf("Run the following command to fix:\n")
		fmt.Printf("  puniversald authz grant <operator-key> %s\n", cfg.AuthzHotkey)
		return fmt.Errorf("hot key verification failed")
	}

	categoryDesc := ""
	switch uauthz.GetMsgTypeCategory() {
	case "default":
		categoryDesc = " with standard Cosmos SDK operations"
	case "universal-validator":
		categoryDesc = " for Universal Validator operations"
	case "custom":
		categoryDesc = " with custom message types"
	}

	fmt.Printf("\n🎉 Hot key setup is COMPLETE and ready%s!\n", categoryDesc)

	return nil
}