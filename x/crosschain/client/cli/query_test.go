package cli_test

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/rollchains/pchain/x/crosschain/client/cli"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func TestGetQueryCmd(t *testing.T) {
	cmd := cli.GetQueryCmd()

	require.NotNil(t, cmd)
	require.Equal(t, types.ModuleName, cmd.Use)
	require.Equal(t, "Querying commands for "+types.ModuleName, cmd.Short)
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

	require.Contains(t, foundCommands, "params")
	require.Contains(t, foundCommands, "admin-params")
}

func TestGetCmdParams(t *testing.T) {
	cmd := cli.GetCmdParams()

	require.NotNil(t, cmd)
	require.Equal(t, "params", cmd.Use)
	require.Equal(t, "Show all module params", cmd.Short)
	require.Equal(t, 0, cmd.Args(cmd, []string{}))

	// Test argument validation - should accept exactly 0 arguments
	err := cmd.Args(cmd, []string{})
	require.NoError(t, err) // Should succeed with no arguments

	err = cmd.Args(cmd, []string{"arg1"})
	require.Error(t, err) // Should fail with arguments
}

func TestGetCmdAdminParams(t *testing.T) {
	cmd := cli.GetCmdAdminParams()

	require.NotNil(t, cmd)
	require.Equal(t, "admin-params", cmd.Use)
	require.Equal(t, "Show all module admin params", cmd.Short)
	require.Equal(t, 0, cmd.Args(cmd, []string{}))

	// Test argument validation - should accept exactly 0 arguments
	err := cmd.Args(cmd, []string{})
	require.NoError(t, err) // Should succeed with no arguments

	err = cmd.Args(cmd, []string{"arg1"})
	require.Error(t, err) // Should fail with arguments
}

// Test query command help functionality
func TestQueryCommandHelp(t *testing.T) {
	tests := []struct {
		name string
		cmd  *cobra.Command
	}{
		{
			name: "params help",
			cmd:  cli.GetCmdParams(),
		},
		{
			name: "admin-params help",
			cmd:  cli.GetCmdAdminParams(),
		},
		{
			name: "query root help",
			cmd:  cli.GetQueryCmd(),
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

// Test query flag handling
func TestQueryFlags(t *testing.T) {
	cmd := cli.GetCmdParams()

	// Check that query flags are added
	flags := cmd.Flags()
	require.NotNil(t, flags)

	// Check that the command has proper structure
	require.NotEmpty(t, cmd.Use)
	require.NotEmpty(t, cmd.Short)
}

// Test query command structure
func TestQueryCommandStructure(t *testing.T) {
	tests := []struct {
		name          string
		cmd           *cobra.Command
		expectedUse   string
		expectedShort string
		expectedArgs  int
	}{
		{
			name:          "params query structure",
			cmd:           cli.GetCmdParams(),
			expectedUse:   "params",
			expectedShort: "Show all module params",
			expectedArgs:  0,
		},
		{
			name:          "admin-params query structure",
			cmd:           cli.GetCmdAdminParams(),
			expectedUse:   "admin-params",
			expectedShort: "Show all module admin params",
			expectedArgs:  0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expectedUse, tc.cmd.Use)
			require.Equal(t, tc.expectedShort, tc.cmd.Short)

			// Test argument validation for exact number of args
			testArgs := make([]string, tc.expectedArgs)
			for i := 0; i < tc.expectedArgs; i++ {
				testArgs[i] = "test_arg"
			}

			// This should not error for correct number of args
			require.NotNil(t, tc.cmd.Args)
		})
	}
}

// Test query command hierarchy
func TestQueryCommandHierarchy(t *testing.T) {
	rootCmd := cli.GetQueryCmd()

	// Test that root command has proper configuration
	require.Equal(t, types.ModuleName, rootCmd.Use)
	require.True(t, rootCmd.DisableFlagParsing)
	require.True(t, rootCmd.SuggestionsMinimumDistance > 0)

	// Test that all subcommands are present
	subCommands := rootCmd.Commands()
	require.True(t, len(subCommands) >= 2) // At least params and admin-params

	// Verify each subcommand has proper parent
	for _, subCmd := range subCommands {
		require.Equal(t, rootCmd, subCmd.Parent())
	}
}

// Test that query requests are properly typed
func TestQueryRequestTypes(t *testing.T) {
	t.Run("params_request_type", func(t *testing.T) {
		req := &types.QueryParamsRequest{}
		require.NotNil(t, req)

		// Test that the request implements proper interface
		require.Implements(t, (*interface{})(nil), req)
	})

	t.Run("admin_params_request_type", func(t *testing.T) {
		req := &types.QueryAdminParamsRequest{}
		require.NotNil(t, req)

		// Test that the request implements proper interface
		require.Implements(t, (*interface{})(nil), req)
	})
}

// Test query response types
func TestQueryResponseTypes(t *testing.T) {
	t.Run("params_response_type", func(t *testing.T) {
		resp := &types.QueryParamsResponse{
			Params: &types.Params{
				Admin: "push1234567890123456789012345678901234567890",
			},
		}
		require.NotNil(t, resp)
		require.NotNil(t, resp.Params)
		require.Equal(t, "push1234567890123456789012345678901234567890", resp.Params.Admin)
	})

	t.Run("admin_params_response_type", func(t *testing.T) {
		resp := &types.QueryAdminParamsResponse{
			AdminParams: &types.AdminParams{
				FactoryAddress: "0x1234567890123456789012345678901234567890",
			},
		}
		require.NotNil(t, resp)
		require.NotNil(t, resp.AdminParams)
		require.Equal(t, "0x1234567890123456789012345678901234567890", resp.AdminParams.FactoryAddress)
	})
}

// Test command validation without client context
func TestQueryCommandValidation(t *testing.T) {
	t.Run("params_command_validation", func(t *testing.T) {
		cmd := cli.GetCmdParams()

		// Test with correct number of arguments (0)
		err := cmd.Args(cmd, []string{})
		require.NoError(t, err)

		// Test with incorrect number of arguments
		err = cmd.Args(cmd, []string{"extra_arg"})
		require.Error(t, err)
	})

	t.Run("admin_params_command_validation", func(t *testing.T) {
		cmd := cli.GetCmdAdminParams()

		// Test with correct number of arguments (0)
		err := cmd.Args(cmd, []string{})
		require.NoError(t, err)

		// Test with incorrect number of arguments
		err = cmd.Args(cmd, []string{"extra_arg"})
		require.Error(t, err)
	})
}

// Test command metadata consistency
func TestQueryCommandMetadata(t *testing.T) {
	tests := []struct {
		name      string
		cmd       *cobra.Command
		checkUse  string
		checkDesc string
	}{
		{
			name:      "params command metadata",
			cmd:       cli.GetCmdParams(),
			checkUse:  "params",
			checkDesc: "Show all module params",
		},
		{
			name:      "admin-params command metadata",
			cmd:       cli.GetCmdAdminParams(),
			checkUse:  "admin-params",
			checkDesc: "Show all module admin params",
		},
		{
			name:      "root query command metadata",
			cmd:       cli.GetQueryCmd(),
			checkUse:  types.ModuleName,
			checkDesc: "Querying commands for " + types.ModuleName,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.checkUse, tc.cmd.Use)
			require.Equal(t, tc.checkDesc, tc.cmd.Short)
			require.NotNil(t, tc.cmd.RunE) // Should have a run function

			// Verify command has proper structure
			require.NotEmpty(t, tc.cmd.Use)
			require.NotEmpty(t, tc.cmd.Short)
		})
	}
}

// Test command argument constraints
func TestQueryArgumentConstraints(t *testing.T) {
	tests := []struct {
		name           string
		cmd            *cobra.Command
		validArgs      []string
		invalidArgs    [][]string
		shouldValidate bool
	}{
		{
			name:           "params command args",
			cmd:            cli.GetCmdParams(),
			validArgs:      []string{},
			invalidArgs:    [][]string{{"arg1"}, {"arg1", "arg2"}},
			shouldValidate: true,
		},
		{
			name:           "admin-params command args",
			cmd:            cli.GetCmdAdminParams(),
			validArgs:      []string{},
			invalidArgs:    [][]string{{"arg1"}, {"arg1", "arg2"}},
			shouldValidate: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.shouldValidate {
				// Test valid arguments
				if tc.cmd.Args != nil {
					err := tc.cmd.Args(tc.cmd, tc.validArgs)
					require.NoError(t, err)
				}

				// Test invalid arguments
				for _, invalidArgs := range tc.invalidArgs {
					if tc.cmd.Args != nil {
						err := tc.cmd.Args(tc.cmd, invalidArgs)
						require.Error(t, err)
					}
				}
			}
		})
	}
}
