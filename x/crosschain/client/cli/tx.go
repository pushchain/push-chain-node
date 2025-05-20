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

			adminAddr := args[1]

			// Validate Bech32 address
			_, err = sdk.AccAddressFromBech32(adminAddr)
			if err != nil {
				return err
			}

			msg := &types.MsgUpdateParams{
				Authority: senderAddress.String(),
				Params: types.Params{
					Admin: adminAddr,
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
		Use:   "update-admin-params [factory-address]",
		Short: "Update the admin params (must be submitted from the admin)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			factoryAddr := args[0]

			msg := &types.MsgUpdateAdminParams{
				Admin: senderAddress.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: factoryAddr,
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
		Use:   "deploy-nmsc [namespace] [chain-id] [owner-key] [vm-type] [tx-hash]",
		Short: "Deploy a new NMSC Smart Account",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			nameSpace := args[0]
			chainId := args[1]
			ownerKey := args[2]
			vmType, err := strconv.ParseUint(args[3], 10, 8)
			if err != nil {
				return err
			}
			txHash := args[4]

			msg := &types.MsgDeployNMSC{
				Signer: senderAddress.String(),
				AccountId: &types.AccountId{
					Namespace: nameSpace,
					ChainId:   chainId,
					OwnerKey:  ownerKey,
					VmType:    types.VM_TYPE(vmType),
				},
				TxHash: txHash,
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
		Use:   "mint-push [namespace] [chain-id] [owner-key] [vm-type] [tx-hash]",
		Short: "Mint Push tokens based on locked amount",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			nameSpace := args[0]
			chainId := args[1]
			ownerKey := args[2]
			vmType, err := strconv.ParseUint(args[3], 10, 8)
			if err != nil {
				return err
			}
			txHash := args[4]

			msg := &types.MsgMintPush{
				Signer: senderAddress.String(),
				AccountId: &types.AccountId{
					Namespace: nameSpace,
					ChainId:   chainId,
					OwnerKey:  ownerKey,
					VmType:    types.VM_TYPE(vmType),
				},
				TxHash: txHash,
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
		Use:   "execute-payload [namespace] [chain-id] [owner-key] [vm-type] [target] [value] [data-hex] [gas-limit] [max-fee-per-gas] [max-priority-fee-per-gas] [nonce] [deadline] [signature-hex]",
		Short: "Execute a cross-chain payload with a signature",
		Args:  cobra.ExactArgs(9),
		RunE: func(cmd *cobra.Command, args []string) error {
			cliCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			senderAddress := cliCtx.GetFromAddress()

			nameSpace := args[0]
			chainId := args[1]
			ownerKey := args[2]
			vmType, err := strconv.ParseUint(args[3], 10, 8)
			if err != nil {
				return err
			}
			target := args[4]
			value := args[5]
			data := args[6]
			gasLimit := args[7]
			maxFeePerGas := args[8]
			maxPriorityFeePerGas := args[9]
			nonce := args[10]
			deadline := args[11]
			signature := args[12]

			msg := &types.MsgExecutePayload{
				Signer: senderAddress.String(),
				AccountId: &types.AccountId{
					Namespace: nameSpace,
					ChainId:   chainId,
					OwnerKey:  ownerKey,
					VmType:    types.VM_TYPE(vmType),
				},
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
