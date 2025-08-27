package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"

	uauthz "github.com/rollchains/pchain/universalClient/authz"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/keys"
)

var (
	operatorFlag string
	hotkeyFlag   string
	rpcEndpoint  string
	chainID      string
)

// authzCmd returns the authz command with all subcommands
func authzCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authz",
		Short: "Manage AuthZ grants for Universal Validator hot keys",
		Long: `
The authz commands allow you to manage authorization grants between operator and hot keys.
This enables the hot key to sign transactions on behalf of the operator account.

Available Commands:
  grant    Grant permissions from operator to hot key
  revoke   Revoke permissions from hot key
  list     List all grants for an account
  verify   Verify hot key has required permissions
  exec     Execute a transaction using AuthZ grants
`,
	}

	// Add persistent flags
	cmd.PersistentFlags().StringVar(&rpcEndpoint, "rpc", "localhost:9090", "gRPC endpoint for Push Chain")
	cmd.PersistentFlags().StringVar(&chainID, "chain-id", "pchain", "Chain ID for transactions")

	// Add subcommands
	cmd.AddCommand(authzGrantCmd())
	cmd.AddCommand(authzRevokeCmd())
	cmd.AddCommand(authzListCmd())
	cmd.AddCommand(authzVerifyCmd())
	cmd.AddCommand(authzExecCmd())

	return cmd
}

// authzGrantCmd grants permissions from operator to hot key
func authzGrantCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "grant <operator-key> <hot-key>",
		Short: "Grant permissions from operator to hot key",
		Long: `
Grant authorization permissions from the operator (validator) key to the hot key.
This allows the hot key to sign Universal Validator transactions on behalf of the operator.

The operator key must have sufficient funds to pay for the transaction.
The hot key will be able to sign specific message types for Universal Validator operations.

Examples:
  puniversald authz grant my-validator my-hotkey
  puniversald authz grant my-validator my-hotkey --rpc localhost:9090
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			operatorKeyName := args[0]
			hotkeyName := args[1]

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

			// Get keyring directory
			keyringDir := config.GetKeyringDir(&cfg)

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, cfg.KeyringBackend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Get operator key
			operatorRecord, err := kb.Key(operatorKeyName)
			if err != nil {
				return fmt.Errorf("operator key '%s' not found: %w", operatorKeyName, err)
			}

			operatorAddr, err := operatorRecord.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get operator address: %w", err)
			}

			// Get hot key
			hotkeyRecord, err := kb.Key(hotkeyName)
			if err != nil {
				return fmt.Errorf("hot key '%s' not found: %w", hotkeyName, err)
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

			// Setup codec
			registry := codectypes.NewInterfaceRegistry()
			cryptocodec.RegisterInterfaces(registry)
			authz.RegisterInterfaces(registry)
			authtypes.RegisterInterfaces(registry)
			cdc := codec.NewProtoCodec(registry)

			// Create client context
			clientCtx := client.Context{}.
				WithCodec(cdc).
				WithInterfaceRegistry(registry).
				WithChainID(chainID).
				WithKeyring(kb).
				WithGRPCClient(conn)

			// Create grants for each allowed message type
			fmt.Printf("Creating AuthZ grants from %s to %s...\n", operatorAddr, hotkeyAddr)

			allowedMsgTypes := uauthz.GetAllAllowedMsgTypes()
			
			var msgs []sdk.Msg
			expiration := time.Now().Add(365 * 24 * time.Hour) // 1 year expiration

			for _, msgType := range allowedMsgTypes {
				// Create generic authorization for this message type
				genericAuth := authz.NewGenericAuthorization(msgType)
				
				// Create MsgGrant
				msgGrant, err := authz.NewMsgGrant(operatorAddr, hotkeyAddr, genericAuth, &expiration)
				if err != nil {
					return fmt.Errorf("failed to create grant for %s: %w", msgType, err)
				}
				
				msgs = append(msgs, msgGrant)
				fmt.Printf("  → Creating grant for: %s\n", msgType)
			}

			// Build and broadcast transaction
			txBuilder := clientCtx.TxConfig.NewTxBuilder()
			err = txBuilder.SetMsgs(msgs...)
			if err != nil {
				return fmt.Errorf("failed to set messages: %w", err)
			}

			// Set gas and fees (you may want to estimate these)
			txBuilder.SetGasLimit(500000) // Adjust as needed
			
			// TODO: Implement signing and broadcasting
			// For now, just display what would be done
			fmt.Printf("\n📋 Transaction Summary:\n")
			fmt.Printf("Messages to send: %d\n", len(msgs))
			fmt.Printf("Estimated gas: %d\n", 500000)
			
			fmt.Printf("\n⚠️  Transaction signing and broadcasting not yet implemented.\n")
			fmt.Printf("This command structure is ready, but needs transaction client setup.\n")
			fmt.Printf("✅ AuthZ grant structure validated successfully!\n")

			// Update config with the hot key settings
			cfg.AuthzGranter = operatorAddr.String()
			cfg.AuthzHotkey = hotkeyName

			if err := config.Save(&cfg, constant.DefaultNodeHome); err != nil {
				fmt.Printf("Warning: Failed to update config with hot key settings: %v\n", err)
			} else {
				fmt.Printf("📝 Config updated with hot key settings\n")
			}

			return nil
		},
	}

	return cmd
}

// authzRevokeCmd revokes permissions from hot key
func authzRevokeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "revoke <operator-key> <hot-key>",
		Short: "Revoke permissions from hot key",
		Long: `
Revoke all authorization permissions from the hot key.
This will disable the hot key from signing transactions on behalf of the operator.

Examples:
  puniversald authz revoke my-validator my-hotkey
`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			operatorKeyName := args[0]
			hotkeyName := args[1]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Get keyring directory
			keyringDir := config.GetKeyringDir(&cfg)

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, cfg.KeyringBackend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Get operator key
			operatorRecord, err := kb.Key(operatorKeyName)
			if err != nil {
				return fmt.Errorf("operator key '%s' not found: %w", operatorKeyName, err)
			}

			operatorAddr, err := operatorRecord.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get operator address: %w", err)
			}

			// Get hot key
			hotkeyRecord, err := kb.Key(hotkeyName)
			if err != nil {
				return fmt.Errorf("hot key '%s' not found: %w", hotkeyName, err)
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

			// Setup codec
			registry := codectypes.NewInterfaceRegistry()
			cryptocodec.RegisterInterfaces(registry)
			authz.RegisterInterfaces(registry)
			authtypes.RegisterInterfaces(registry)
			cdc := codec.NewProtoCodec(registry)

			// Create client context
			clientCtx := client.Context{}.
				WithCodec(cdc).
				WithInterfaceRegistry(registry).
				WithChainID(chainID).
				WithKeyring(kb).
				WithGRPCClient(conn)

			// Create revoke messages for each allowed message type
			fmt.Printf("Revoking AuthZ grants from %s to %s...\n", operatorAddr, hotkeyAddr)

			allowedMsgTypes := uauthz.GetAllAllowedMsgTypes()
			
			var msgs []sdk.Msg

			for _, msgType := range allowedMsgTypes {
				// Create MsgRevoke
				msgRevoke := authz.NewMsgRevoke(operatorAddr, hotkeyAddr, msgType)
				msgs = append(msgs, &msgRevoke)
				fmt.Printf("  → Revoking grant for: %s\n", msgType)
			}

			// Build and broadcast transaction
			txBuilder := clientCtx.TxConfig.NewTxBuilder()
			err = txBuilder.SetMsgs(msgs...)
			if err != nil {
				return fmt.Errorf("failed to set messages: %w", err)
			}

			// Set gas and fees
			txBuilder.SetGasLimit(300000)
			
			// TODO: Implement signing and broadcasting
			// For now, just display what would be done
			fmt.Printf("\n📋 Transaction Summary:\n")
			fmt.Printf("Messages to send: %d\n", len(msgs))
			fmt.Printf("Estimated gas: %d\n", 300000)
			
			fmt.Printf("\n⚠️  Transaction signing and broadcasting not yet implemented.\n")
			fmt.Printf("This command structure is ready, but needs transaction client setup.\n")
			fmt.Printf("✅ AuthZ revoke structure validated successfully!\n")

			return nil
		},
	}

	return cmd
}

// authzListCmd lists all grants for an account
func authzListCmd() *cobra.Command {
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
			account := args[0]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			var accountAddr sdk.AccAddress

			// Try to parse as address first, then as key name
			if addr, err := sdk.AccAddressFromBech32(account); err == nil {
				accountAddr = addr
			} else {
				// Try as key name
				keyringDir := config.GetKeyringDir(&cfg)
				kb, err := getKeybase(keyringDir, nil, cfg.KeyringBackend)
				if err != nil {
					return fmt.Errorf("failed to create keyring: %w", err)
				}

				record, err := kb.Key(account)
				if err != nil {
					return fmt.Errorf("account '%s' not found as address or key name: %w", account, err)
				}

				accountAddr, err = record.GetAddress()
				if err != nil {
					return fmt.Errorf("failed to get address from key: %w", err)
				}
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
		},
	}

	return cmd
}

// authzVerifyCmd verifies hot key has required permissions
func authzVerifyCmd() *cobra.Command {
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
			keyringDir := config.GetKeyringDir(&cfg)
			kb, err := getKeybase(keyringDir, nil, cfg.KeyringBackend)
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
		},
	}

	return cmd
}

// Message creation helper functions

// createMsgSend creates a MsgSend message
func createMsgSend(fromAddr, toAddr sdk.AccAddress, amount sdk.Coins) sdk.Msg {
	return &banktypes.MsgSend{
		FromAddress: fromAddr.String(),
		ToAddress:   toAddr.String(),
		Amount:      amount,
	}
}

// createMsgDelegate creates a MsgDelegate message
func createMsgDelegate(delegatorAddr sdk.AccAddress, validatorAddr string, amount sdk.Coin) sdk.Msg {
	return &stakingtypes.MsgDelegate{
		DelegatorAddress: delegatorAddr.String(),
		ValidatorAddress: validatorAddr,
		Amount:           amount,
	}
}

// createMsgUndelegate creates a MsgUndelegate message
func createMsgUndelegate(delegatorAddr sdk.AccAddress, validatorAddr string, amount sdk.Coin) sdk.Msg {
	return &stakingtypes.MsgUndelegate{
		DelegatorAddress: delegatorAddr.String(),
		ValidatorAddress: validatorAddr,
		Amount:           amount,
	}
}


// createMsgVote creates a MsgVote message
func createMsgVote(voterAddr sdk.AccAddress, proposalID uint64, option govtypes.VoteOption) sdk.Msg {
	return &govtypes.MsgVote{
		ProposalId: proposalID,
		Voter:      voterAddr.String(),
		Option:     option,
	}
}

// authzExecCmd executes a transaction using AuthZ
func authzExecCmd() *cobra.Command {
	var gasLimit uint64 = 300000
	var feeAmount string = "1000push"
	var memo string

	cmd := &cobra.Command{
		Use:   "exec <grantee-key> <msg-type> [args...]",
		Short: "Execute a transaction using AuthZ grants",
		Long: `
Execute a transaction on behalf of the granter using AuthZ permissions.
The grantee (hot key) must have been granted permission to execute the specified message type.

Supported message types (matching grants created by setup-container-authz):
  /cosmos.bank.v1beta1.MsgSend           - <to-addr> <amount>
  /cosmos.staking.v1beta1.MsgDelegate    - <validator> <amount>
  /cosmos.staking.v1beta1.MsgUndelegate  - <validator> <amount>
  /cosmos.gov.v1beta1.MsgVote           - <proposal-id> <option>

Examples:
  puniversald authz exec container-hotkey /cosmos.bank.v1beta1.MsgSend push1abc... 1000push
  puniversald authz exec container-hotkey /cosmos.staking.v1beta1.MsgDelegate pushvaloper1abc... 1000000push
  puniversald authz exec container-hotkey /cosmos.staking.v1beta1.MsgUndelegate pushvaloper1abc... 1000000push
  puniversald authz exec container-hotkey /cosmos.gov.v1beta1.MsgVote 1 yes
`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			granteeKeyName := args[0]
			msgType := args[1]
			msgArgs := args[2:]

			// Load config
			cfg, err := config.Load(constant.DefaultNodeHome)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}

			// Validate hot key configuration
			if err := config.ValidateHotKeyConfig(&cfg); err != nil {
				return fmt.Errorf("hot key configuration is invalid: %w", err)
			}

			// Parse operator address
			granterAddr, err := sdk.AccAddressFromBech32(cfg.AuthzGranter)
			if err != nil {
				return fmt.Errorf("invalid operator address: %w", err)
			}

			// Get keyring directory
			keyringDir := config.GetKeyringDir(&cfg)

			// Create keyring
			kb, err := getKeybase(keyringDir, nil, cfg.KeyringBackend)
			if err != nil {
				return fmt.Errorf("failed to create keyring: %w", err)
			}

			// Get grantee key
			granteeRecord, err := kb.Key(granteeKeyName)
			if err != nil {
				return fmt.Errorf("grantee key '%s' not found: %w", granteeKeyName, err)
			}

			granteeAddr, err := granteeRecord.GetAddress()
			if err != nil {
				return fmt.Errorf("failed to get grantee address: %w", err)
			}

			// Create the inner message based on type
			var innerMsg sdk.Msg
			switch msgType {
			case "/cosmos.bank.v1beta1.MsgSend":
				if len(msgArgs) < 2 {
					return fmt.Errorf("MsgSend requires: <to-address> <amount>")
				}
				toAddr, err := sdk.AccAddressFromBech32(msgArgs[0])
				if err != nil {
					return fmt.Errorf("invalid to address: %w", err)
				}
				amount, err := sdk.ParseCoinsNormalized(msgArgs[1])
				if err != nil {
					return fmt.Errorf("invalid amount: %w", err)
				}
				innerMsg = createMsgSend(granterAddr, toAddr, amount)

			case "/cosmos.staking.v1beta1.MsgDelegate":
				if len(msgArgs) < 2 {
					return fmt.Errorf("MsgDelegate requires: <validator> <amount>")
				}
				validatorAddr := msgArgs[0]
				amount, err := sdk.ParseCoinNormalized(msgArgs[1])
				if err != nil {
					return fmt.Errorf("invalid amount: %w", err)
				}
				innerMsg = createMsgDelegate(granterAddr, validatorAddr, amount)

			case "/cosmos.staking.v1beta1.MsgUndelegate":
				if len(msgArgs) < 2 {
					return fmt.Errorf("MsgUndelegate requires: <validator> <amount>")
				}
				validatorAddr := msgArgs[0]
				amount, err := sdk.ParseCoinNormalized(msgArgs[1])
				if err != nil {
					return fmt.Errorf("invalid amount: %w", err)
				}
				innerMsg = createMsgUndelegate(granterAddr, validatorAddr, amount)

			case "/cosmos.gov.v1beta1.MsgVote":
				if len(msgArgs) < 2 {
					return fmt.Errorf("MsgVote requires: <proposal-id> <option>")
				}
				proposalID, err := strconv.ParseUint(msgArgs[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid proposal ID: %w", err)
				}
				
				var option govtypes.VoteOption
				switch msgArgs[1] {
				case "yes":
					option = govtypes.OptionYes
				case "no":
					option = govtypes.OptionNo
				case "abstain":
					option = govtypes.OptionAbstain
				case "no_with_veto":
					option = govtypes.OptionNoWithVeto
				default:
					return fmt.Errorf("invalid vote option: %s (use: yes, no, abstain, no_with_veto)", msgArgs[1])
				}
				innerMsg = createMsgVote(granterAddr, proposalID, option)

			default:
				return fmt.Errorf("unsupported message type: %s", msgType)
			}

			// Create gRPC connection
			conn, err := grpc.Dial(rpcEndpoint, grpc.WithInsecure())
			if err != nil {
				return fmt.Errorf("failed to connect to gRPC endpoint: %w", err)
			}
			defer conn.Close()

			// Setup codec with all required interfaces
			registry := codectypes.NewInterfaceRegistry()
			cryptocodec.RegisterInterfaces(registry)
			authz.RegisterInterfaces(registry)
			authtypes.RegisterInterfaces(registry)
			banktypes.RegisterInterfaces(registry)
			stakingtypes.RegisterInterfaces(registry)
			govtypes.RegisterInterfaces(registry)
			cdc := codec.NewProtoCodec(registry)

			// Create TxConfig
			txConfig := tx.NewTxConfig(cdc, []signing.SignMode{signing.SignMode_SIGN_MODE_DIRECT})

			// Create client context
			clientCtx := client.Context{}.
				WithCodec(cdc).
				WithInterfaceRegistry(registry).
				WithChainID(chainID).
				WithKeyring(kb).
				WithGRPCClient(conn).
				WithFromAddress(granteeAddr).
				WithFromName(granteeKeyName).
				WithTxConfig(txConfig)

			// Create keys instance for the hot key
			hotKeys := keys.NewKeysWithKeybase(kb, granteeAddr, granteeKeyName, "")

			// Setup AuthZ signer configuration
			uauthz.SetupAuthZSignerList(granterAddr.String(), granteeAddr)
			
			// Get signer for this message type
			signer, err := uauthz.GetSigner(msgType)
			if err != nil {
				return fmt.Errorf("failed to get AuthZ signer: %w", err)
			}

			// Create TxSigner for handling the transaction  
			logger := zerolog.New(nil).Level(zerolog.InfoLevel)
			txSigner := uauthz.NewTxSigner(hotKeys, &signer, clientCtx, logger)

			// Parse fee amount
			feeCoins, err := sdk.ParseCoinsNormalized(feeAmount)
			if err != nil {
				return fmt.Errorf("invalid fee amount: %w", err)
			}

			fmt.Printf("\n🚀 Executing AuthZ Transaction:\n")
			fmt.Printf("Granter (operator): %s\n", granterAddr)
			fmt.Printf("Grantee (executor): %s (%s)\n", granteeAddr, granteeKeyName)
			fmt.Printf("Message Type: %s\n", msgType)
			fmt.Printf("Gas Limit: %d\n", gasLimit)
			fmt.Printf("Fee Amount: %s\n", feeCoins)
			fmt.Printf("Memo: %s\n", memo)

			// Execute the transaction
			res, err := txSigner.SignAndBroadcastAuthZTx(ctx, []sdk.Msg{innerMsg}, memo, gasLimit, feeCoins)
			if err != nil {
				return fmt.Errorf("failed to execute AuthZ transaction: %w", err)
			}

			// Print transaction result
			fmt.Printf("\n✅ Transaction Successful!\n")
			fmt.Printf("Transaction Hash: %s\n", res.TxHash)
			fmt.Printf("Gas Used: %d\n", res.GasUsed)
			fmt.Printf("Gas Wanted: %d\n", res.GasWanted)
			if res.Code != 0 {
				fmt.Printf("⚠️  Transaction failed with code %d: %s\n", res.Code, res.RawLog)
			} else {
				fmt.Printf("🎉 Transaction executed successfully!\n")
			}

			return nil
		},
	}

	// Add command flags
	cmd.Flags().Uint64Var(&gasLimit, "gas", 300000, "Gas limit for the transaction")
	cmd.Flags().StringVar(&feeAmount, "fees", "1000push", "Fee amount for the transaction")
	cmd.Flags().StringVar(&memo, "memo", "", "Memo for the transaction")

	return cmd
}