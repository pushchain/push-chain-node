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
		Use:   "update-admin-params [some-value]",
		Short: "Update the admin params (must be submitted from the admin)",
		Args:  cobra.ExactArgs(1),
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
