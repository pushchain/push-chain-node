package cli

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/x/crosschain/types"
)

// !NOTE: Must enable in module.go (disabled in favor of autocli.go)

// NewTxCmd returns a root CLI command handler for certain modules
// transaction commands.
func NewTxCmd() *cobra.Command {
	txCmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      types.ModuleName + " subcommands.",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	txCmd.AddCommand(
		MsgUpdateParams(),
		MsgUpdateAdminParams(),
		MsgDeployNMSC(),
	)
	return txCmd
}

// Returns a CLI command handler for registering a
// contract for the module.
func MsgUpdateParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-params [some-value]",
		Short: "Update the params (must be submitted from the authority)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			someValue, err := strconv.ParseBool(args[0])
			if err != nil {
				return err
			}

			adminAddr := args[1]

			// Validate Bech32 address
			_, err = sdk.AccAddressFromBech32(adminAddr)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateParams{
				Authority: senderAddress.String(),
				Params: types.Params{
					SomeValue: someValue,
					Admin:     adminAddr,
				},
			}

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// Returns a CLI command handler for registering a
// contract for the module.
func MsgUpdateAdminParams() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-admin-params [factory-address] [verifier-precompile]",
		Short: "Update the admin params (must be submitted from the admin)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			factoryAddr := args[0]
			verifierPrecompile := args[1]

			msg := &types.MsgUpdateAdminParams{
				Admin: senderAddress.String(),
				AdminParams: types.AdminParams{
					FactoryAddress:     factoryAddr,
					VerifierPrecompile: verifierPrecompile,
				},
			}

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func MsgDeployNMSC() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "deploy-nmsc [user-key] [caip-string] [owner-type] [tx-hash]",
		Short: "Deploy a new NMSC Smart Account",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			userKey := args[0]
			caipString := args[1]
			ownerType, err := strconv.ParseUint(args[2], 10, 8)
			if err != nil {
				return err
			}
			txHash := args[3]

			msg := &types.MsgDeployNMSC{
				Signer:     senderAddress.String(),
				UserKey:    userKey,
				CaipString: caipString,
				OwnerType:  uint32(ownerType),
				TxHash:     txHash,
			}

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func MsgMintPush() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mint-push [tx-hash] [caip-string]",
		Short: "Mint Push tokens based on locked amount",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			txHash := args[0]
			caipString := args[1]

			msg := &types.MsgMintPush{
				Signer:     senderAddress.String(),
				TxHash:     txHash,
				CaipString: caipString,
			}

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func MsgExecutePayload() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "execute-payload [caip-string] [target] [value] [data-hex] [gas-limit] [max-fee-per-gas] [max-priority-fee-per-gas] [nonce] [deadline] [signature-hex]",
		Short: "Execute a cross-chain payload with a signature",
		Args:  cobra.ExactArgs(9),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			caipString := args[0]
			target := args[1]
			value := args[2]
			data := args[3]
			gasLimit := args[4]
			maxFeePerGas := args[5]
			maxPriorityFeePerGas := args[6]
			nonce := args[7]
			deadline := args[8]
			signature := args[9]

			msg := &types.MsgExecutePayload{
				Signer:     senderAddress.String(),
				CaipString: caipString,
				CrosschainPayload: &types.CrossChainPayload{
					Target:               target,
					Value:                value,
					Data:                 data,
					GasLimit:             gasLimit,
					MaxFeePerGas:         maxFeePerGas,
					MaxPriorityFeePerGas: maxPriorityFeePerGas,
					Nonce:                nonce,
					Deadline:             deadline,
				},
				Signature: signature,
			}

			if err := msg.Validate(); err != nil {
				return err
			}

			return tx.GenerateOrBroadcastTxCLI(cliCtx, cmd.Flags(), msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
