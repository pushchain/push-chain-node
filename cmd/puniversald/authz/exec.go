package authz

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/rs/zerolog"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"

	uauthz "github.com/rollchains/pchain/universalClient/authz"
	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rollchains/pchain/universalClient/constant"
	"github.com/rollchains/pchain/universalClient/keys"
)

// ExecCmd creates the authz exec command
func ExecCmd(rpcEndpoint, chainID *string) *cobra.Command {
	var gasLimit uint64 = 300000
	var feeAmount string = "300000000000000upc"
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
			return runExecCommand(args[0], args[1], args[2:], *rpcEndpoint, *chainID, gasLimit, feeAmount, memo)
		},
	}

	// Add command flags
	cmd.Flags().Uint64Var(&gasLimit, "gas", 300000, "Gas limit for the transaction")
	cmd.Flags().StringVar(&feeAmount, "fees", "300000000000000upc", "Fee amount for the transaction")
	cmd.Flags().StringVar(&memo, "memo", "", "Memo for the transaction")

	return cmd
}

func runExecCommand(granteeKeyName, msgType string, msgArgs []string, rpcEndpoint, chainID string, gasLimit uint64, feeAmount, memo string) error {
	ctx := context.Background()

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

	// Use the same keyring directory as the EVM key commands
	keyringDir := constant.DefaultNodeHome

	// Create keyring using universalClient keyring (compatible with pchaind)
	keyConfig := keys.KeyringConfig{
		HomeDir:        keyringDir,
		KeyringBackend: keys.KeyringBackend(cfg.KeyringBackend),
		HotkeyName:     granteeKeyName,
	}
	kb, _, err := keys.GetKeyringKeybase(keyConfig)
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
	messageFactory := NewMessageFactory()
	innerMsg, err := messageFactory.ParseMessageFromArgs(msgType, granterAddr, msgArgs)
	if err != nil {
		return err
	}

	// Setup client context
	clientCtx, err := setupClientContextForExec(keyringDir, kb, cfg.KeyringBackend, chainID, rpcEndpoint, granteeAddr, granteeKeyName)
	if err != nil {
		return fmt.Errorf("failed to setup client context: %w", err)
	}

	// Create keys instance for the hot key
	hotKeys := keys.NewKeysWithKeybase(kb, granteeAddr, granteeKeyName, "")

	// Create SignerManager for AuthZ
	signerManager := uauthz.NewSignerManager(granterAddr.String(), granteeAddr)

	// Create TxSigner for handling the transaction  
	logger := zerolog.New(nil).Level(zerolog.InfoLevel)
	txSigner := uauthz.NewTxSigner(hotKeys, signerManager, clientCtx, logger)

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
}

// setupClientContextForExec creates a client context specifically for exec command with HTTP client
func setupClientContextForExec(keyringDir string, kb keyring.Keyring, keyringBackend config.KeyringBackend, chainID, rpcEndpoint string, granteeAddr sdk.AccAddress, granteeKeyName string) (client.Context, error) {
	// Setup basic client context
	clientCtx, err := setupClientContext(keyringDir, kb, keyringBackend, chainID, rpcEndpoint)
	if err != nil {
		return client.Context{}, err
	}

	// Create HTTP RPC client for broadcasting
	rpcURL := fmt.Sprintf("http://%s", rpcEndpoint)
	if rpcEndpoint == "core-validator-1:9090" {
		rpcURL = "http://core-validator-1:26657"  // Use RPC port instead of gRPC port
	}
	httpClient, err := rpchttp.New(rpcURL, "/websocket")
	if err != nil {
		return client.Context{}, fmt.Errorf("failed to create RPC client: %w", err)
	}

	// Enhance client context with additional settings for exec
	return clientCtx.
		WithClient(httpClient).
		WithFromAddress(granteeAddr).
		WithFromName(granteeKeyName).
		WithBroadcastMode("sync"), nil
}