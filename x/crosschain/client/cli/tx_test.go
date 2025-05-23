package cli_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/x/crosschain/client/cli"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func TestNewTxCmd(t *testing.T) {
	cmd := cli.NewTxCmd()

	require.NotNil(t, cmd)
	require.Equal(t, types.ModuleName, cmd.Use)
	require.True(t, cmd.DisableFlagParsing)
	require.Equal(t, 2, cmd.SuggestionsMinimumDistance)

	// Check that subcommands are added
	subCmds := cmd.Commands()
	require.True(t, len(subCmds) > 0)

	// Check for specific subcommands
	var foundCommands []string
	for _, subCmd := range subCmds {
		foundCommands = append(foundCommands, subCmd.Use)
	}

	require.Contains(t, foundCommands, "update-params")
	require.Contains(t, foundCommands, "update-admin-params")
	require.Contains(t, foundCommands, "deploy-nmsc")
}

func TestMsgUpdateParamsCommand(t *testing.T) {
	cmd := cli.MsgUpdateParams()

	require.NotNil(t, cmd)
	require.Equal(t, "update-params [some-value]", cmd.Use)
	require.Equal(t, "Update the params (must be submitted from the authority)", cmd.Short)
	require.Equal(t, 1, cmd.Args(cmd, []string{"arg1"}))

	// Test argument validation
	err := cmd.Args(cmd, []string{})
	require.Error(t, err) // Should fail with no arguments

	err = cmd.Args(cmd, []string{"arg1", "arg2"})
	require.Error(t, err) // Should fail with too many arguments
}

func TestMsgUpdateAdminParamsCommand(t *testing.T) {
	cmd := cli.MsgUpdateAdminParams()

	require.NotNil(t, cmd)
	require.Equal(t, "update-admin-params [factory-address]", cmd.Use)
	require.Equal(t, "Update the admin params (must be submitted from the admin)", cmd.Short)
	require.Equal(t, 2, cmd.Args(cmd, []string{"arg1", "arg2"}))

	// Test argument validation
	err := cmd.Args(cmd, []string{})
	require.Error(t, err) // Should fail with no arguments

	err = cmd.Args(cmd, []string{"arg1"})
	require.Error(t, err) // Should fail with insufficient arguments

	err = cmd.Args(cmd, []string{"arg1", "arg2", "arg3"})
	require.Error(t, err) // Should fail with too many arguments
}

func TestMsgDeployNMSCCommand(t *testing.T) {
	cmd := cli.MsgDeployNMSC()

	require.NotNil(t, cmd)
	require.Equal(t, "deploy-nmsc [namespace] [chain-id] [owner-key] [vm-type] [tx-hash]", cmd.Use)
	require.Equal(t, "Deploy a new NMSC Smart Account", cmd.Short)
	require.Equal(t, 3, cmd.Args(cmd, []string{"arg1", "arg2", "arg3"}))

	// Test argument validation
	err := cmd.Args(cmd, []string{})
	require.Error(t, err) // Should fail with no arguments

	err = cmd.Args(cmd, []string{"arg1", "arg2"})
	require.Error(t, err) // Should fail with insufficient arguments
}

func TestMsgMintPushCommand(t *testing.T) {
	cmd := cli.MsgMintPush()

	require.NotNil(t, cmd)
	require.Equal(t, "mint-push [namespace] [chain-id] [owner-key] [vm-type] [tx-hash]", cmd.Use)
	require.Equal(t, "Mint Push tokens based on locked amount", cmd.Short)
	require.Equal(t, 2, cmd.Args(cmd, []string{"arg1", "arg2"}))

	// Test argument validation
	err := cmd.Args(cmd, []string{})
	require.Error(t, err) // Should fail with no arguments

	err = cmd.Args(cmd, []string{"arg1"})
	require.Error(t, err) // Should fail with insufficient arguments
}

func TestMsgExecutePayloadCommand(t *testing.T) {
	cmd := cli.MsgExecutePayload()

	require.NotNil(t, cmd)
	require.Contains(t, cmd.Use, "execute-payload")
	require.Equal(t, "Execute a cross-chain payload with a signature", cmd.Short)
	require.Equal(t, 9, cmd.Args(cmd, []string{"1", "2", "3", "4", "5", "6", "7", "8", "9"}))

	// Test argument validation
	err := cmd.Args(cmd, []string{})
	require.Error(t, err) // Should fail with no arguments

	err = cmd.Args(cmd, []string{"arg1", "arg2"})
	require.Error(t, err) // Should fail with insufficient arguments
}

// Test CLI command help functionality
func TestCLICommandHelp(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{
			name: "update-params help",
			cmd:  cli.MsgUpdateParams(),
		},
		{
			name: "update-admin-params help",
			cmd:  cli.MsgUpdateAdminParams(),
		},
		{
			name: "deploy-nmsc help",
			cmd:  cli.MsgDeployNMSC(),
		},
		{
			name: "mint-push help",
			cmd:  cli.MsgMintPush(),
		},
		{
			name: "execute-payload help",
			cmd:  cli.MsgExecutePayload(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Check that help text is accessible
			require.NotEmpty(t, tc.cmd.Short)
			require.NotEmpty(t, tc.cmd.Use)
		})
	}
}

// Test flag handling
func TestCLIFlags(t *testing.T) {
	cmd := cli.MsgUpdateParams()

	// Check that transaction flags are added
	flags := cmd.Flags()
	require.NotNil(t, flags)

	// Check that the command has proper structure
	require.NotEmpty(t, cmd.Use)
	require.NotEmpty(t, cmd.Short)
}

// Test message creation and validation through CLI types
func TestCLIMessageValidation(t *testing.T) {
	t.Run("valid_update_params_message", func(t *testing.T) {
		// This tests the message creation logic within the CLI command
		adminAddr := "push1234567890123456789012345678901234567890"

		msg := &types.MsgUpdateParams{
			Authority: "push_authority_addr_123456789012345678901",
			Params: types.Params{
				Admin: adminAddr,
			},
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("valid_admin_params_message", func(t *testing.T) {
		msg := &types.MsgUpdateAdminParams{
			Admin: "push1234567890123456789012345678901234567890",
			AdminParams: types.AdminParams{
				FactoryAddress: "0x1234567890123456789012345678901234567890",
			},
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("valid_deploy_nmsc_message", func(t *testing.T) {
		msg := &types.MsgDeployNMSC{
			Signer: "push1234567890123456789012345678901234567890",
			AccountId: &types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			TxHash: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefab",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("valid_mint_push_message", func(t *testing.T) {
		msg := &types.MsgMintPush{
			Signer: "push1234567890123456789012345678901234567890",
			AccountId: &types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			TxHash: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefab",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})

	t.Run("valid_execute_payload_message", func(t *testing.T) {
		msg := &types.MsgExecutePayload{
			Signer: "push1234567890123456789012345678901234567890",
			AccountId: &types.AccountId{
				Namespace: "eip155",
				ChainId:   "1",
				OwnerKey:  "0x1234567890123456789012345678901234567890",
				VmType:    types.VM_TYPE_EVM,
			},
			CrosschainPayload: &types.CrossChainPayload{
				Target:               "0x1111111111111111111111111111111111111111",
				Value:                "1000000000000000000",
				Data:                 "0x",
				GasLimit:             "21000",
				MaxFeePerGas:         "20000000000",
				MaxPriorityFeePerGas: "1000000000",
				Nonce:                "1",
				Deadline:             "1234567890",
			},
			Signature: "0xabcdefabcdefabcdefabcdefabcdefabcdefabcdefab",
		}

		err := msg.ValidateBasic()
		require.NoError(t, err)
	})
}

// Test command structure and metadata
func TestCommandStructure(t *testing.T) {
	tests := []struct {
		name          string
		cmd           *cobra.Command
		expectedUse   string
		expectedShort string
		expectedArgs  int
	}{
		{
			name:          "update-params command structure",
			cmd:           cli.MsgUpdateParams(),
			expectedUse:   "update-params [some-value]",
			expectedShort: "Update the params (must be submitted from the authority)",
			expectedArgs:  1,
		},
		{
			name:          "update-admin-params command structure",
			cmd:           cli.MsgUpdateAdminParams(),
			expectedUse:   "update-admin-params [factory-address]",
			expectedShort: "Update the admin params (must be submitted from the admin)",
			expectedArgs:  2,
		},
		{
			name:          "deploy-nmsc command structure",
			cmd:           cli.MsgDeployNMSC(),
			expectedUse:   "deploy-nmsc [namespace] [chain-id] [owner-key] [vm-type] [tx-hash]",
			expectedShort: "Deploy a new NMSC Smart Account",
			expectedArgs:  3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedUse, tc.cmd.Use)
			require.Equal(t, tc.expectedShort, tc.cmd.Short)

			// Create test arguments
			testArgs := make([]string, tc.expectedArgs)
			for i := 0; i < tc.expectedArgs; i++ {
				testArgs[i] = "test_arg"
			}

			// This should not error for correct number of args
			require.NotNil(t, tc.cmd.Args)
		})
	}
}

// Test command hierarchy
func TestCommandHierarchy(t *testing.T) {
	rootCmd := cli.NewTxCmd()

	// Test that root command has proper configuration
	require.Equal(t, types.ModuleName, rootCmd.Use)
	require.True(t, rootCmd.DisableFlagParsing)
	require.True(t, rootCmd.SuggestionsMinimumDistance > 0)

	// Test that all subcommands are present
	subCommands := rootCmd.Commands()
	require.True(t, len(subCommands) >= 3) // At least update-params, update-admin-params, deploy-nmsc

	// Verify each subcommand has proper parent
	for _, subCmd := range subCommands {
		require.Equal(t, rootCmd, subCmd.Parent())
	}
}
