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

	uauthz "github.com/pushchain/push-chain-node/universalClient/authz"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/keys"
)

// ExecCmd creates the authz exec command
func ExecCmd(rpcEndpoint, chainID *string) *cobra.Command {
	var gasLimit uint64 = 300000
	var feeAmount = "300000000000000upc"
	var memo string

	cmd := &cobra.Command{
		Use:   "exec <grantee-key> <msg-type> [args...]",
		Short: "Execute a transaction using AuthZ grants",
		Long: `
Execute a transaction using AuthZ permissions.
The grantee (hot key) must have been granted permission to execute the specified message type.

Supported message types:
  /ue.v1.MsgVoteInbound - <signer> <source-chain> <tx-hash> <sender> <recipient> <amount> <asset-addr> <log-index> <tx-type>

Example:
  puniversald authz exec container-hotkey /ue.v1.MsgVoteInbound push1signer... eip155:11155111 0x123abc 0xsender 0xrecipient 1000 0xasset 1 1
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

	// Load config for keyring settings
	cfg, err := config.Load(constant.DefaultNodeHome)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
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
	innerMsg, err := ParseMessageFromArgs(msgType, msgArgs)
	if err != nil {
		return err
	}

	// Setup client context
	clientCtx, err := setupClientContextForExec(kb, chainID, rpcEndpoint, granteeAddr, granteeKeyName)
	if err != nil {
		return fmt.Errorf("failed to setup client context: %w", err)
	}

	// Create keys instance for the hot key
	hotKeys := keys.NewKeysWithKeybase(kb, granteeAddr, granteeKeyName, "")

	// Create TxSigner for handling the transaction  
	logger := zerolog.New(nil).Level(zerolog.InfoLevel)
	txSigner := uauthz.NewTxSigner(hotKeys, clientCtx, logger)

	// Parse fee amount
	feeCoins, err := sdk.ParseCoinsNormalized(feeAmount)
	if err != nil {
		return fmt.Errorf("invalid fee amount: %w", err)
	}

	fmt.Printf("🚀 AuthZ TX: executor=%s(%s) type=%s gas=%d fee=%s memo=%s\n", granteeAddr, granteeKeyName, msgType, gasLimit, feeCoins, memo)

	// Execute the transaction
	res, err := txSigner.SignAndBroadcastAuthZTx(ctx, []sdk.Msg{innerMsg}, memo, gasLimit, feeCoins)
	if err != nil {
		return fmt.Errorf("failed to execute AuthZ transaction: %w", err)
	}

	// Print transaction result
	if res.Code != 0 {
		fmt.Printf("⚠️ TX Failed (code %d): hash=%s error=%s\n", res.Code, res.TxHash, res.RawLog)
	} else {
		fmt.Printf("✅ TX Success: hash=%s gasUsed=%d/%d\n", res.TxHash, res.GasUsed, res.GasWanted)
	}

	return nil
}

// setupClientContextForExec creates a client context specifically for exec command with HTTP client
func setupClientContextForExec(kb keyring.Keyring, chainID, rpcEndpoint string, granteeAddr sdk.AccAddress, granteeKeyName string) (client.Context, error) {
	// Assume rpcEndpoint is a clean base URL, append standard ports
	// Setup basic client context with gRPC (standard port 9090)
	grpcEndpoint := rpcEndpoint + ":9090"
	clientCtx, err := setupClientContext(kb, chainID, grpcEndpoint)
	if err != nil {
		return client.Context{}, err
	}

	// Create HTTP RPC client for broadcasting (standard port 26657)
	rpcURL := "http://" + rpcEndpoint + ":26657"
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