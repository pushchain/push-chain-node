package keeper_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/x/crosschain/types"
)

func TestUpdateAdminParams(t *testing.T) {
	f := SetupTest(t)
	require := require.New(t)

	// Initialize params with default values
	err := f.k.Params.Set(f.ctx, types.DefaultParams())
	require.NoError(err)

	// Get the default admin address from params
	params, err := f.k.Params.Get(f.ctx)
	require.NoError(err)

	// Make sure we have a valid admin in test context
	adminAddr, err := sdk.AccAddressFromBech32(params.Admin)
	require.NoError(err)

	// Setup non-admin address for authorization tests
	nonAdminAddr := f.addrs[1]
	require.NotEqual(adminAddr.String(), nonAdminAddr.String())

	testCases := []struct {
		name          string
		msg           *types.MsgUpdateAdminParams
		expectError   bool
		errorContains string
	}{
		{
			name: "success: valid admin and factory address",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError: false,
		},
		{
			name: "failure: non-admin address",
			msg: &types.MsgUpdateAdminParams{
				Admin: nonAdminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError:   true,
			errorContains: "unauthorized",
		},
		// BUG 1: Invalid hex characters accepted
		{
			name: "bug 1: invalid hex address with ZZZ characters accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0xZZZF3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 2: Empty factory address accepted
		{
			name: "bug 2: empty factory address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 3: Zero address accepted
		{
			name: "bug 3: zero address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x0000000000000000000000000000000000000000",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 4: Oversized address accepted
		{
			name: "bug 4: oversized address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53CfA83F7689885995606F93b61640000000000000000000000000000000000000000",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 5: Unicode escape sequence accepted
		{
			name: "bug 5: unicode escape sequence accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53CfA83F7689885995606F93b6164\u0000backdoor",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 6: URL/Protocol scheme accepted
		{
			name: "bug 6: URL/Protocol scheme accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "http://malicious.com",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 7: Extremely long address accepted
		{
			name: "bug 7: extremely long address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53CfA83F7689885995606F93b6164527F3692F5C53CfA83F7689885995606F93b6164527F3692F5C53CfA83F7689885995606F93b6164527F3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 8: Missing 0x prefix accepted
		{
			name: "bug 8: missing 0x prefix accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "527F3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 9: Control characters in address accepted
		{
			name: "bug 9: control characters in address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C53\nCfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 10: Precompiled contract address accepted
		{
			name: "bug 10: precompiled contract address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x0000000000000000000000000000000000000001",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 11: Bech32 address format accepted
		{
			name: "bug 11: bech32 address format accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 12: Uppercase X in prefix accepted
		{
			name: "bug 12: uppercase X in 0X prefix accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0X527F3692F5C53CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 13: Non-checksummed address accepted
		{
			name: "bug 13: non-checksummed address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527f3692f5c53cfa83f7689885995606f93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 14: Address with whitespace accepted
		{
			name: "bug 14: address with whitespace accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C5 3CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 15: JavaScript injection attempt accepted
		{
			name: "bug 15: javascript injection attempt accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x<script>alert(1)</script>",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 16: SQL injection attempt accepted
		{
			name: "bug 16: sql injection attempt accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x'; DROP TABLE params; --",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 17: Special characters in address accepted
		{
			name: "bug 17: special characters in address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C5$CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 18: Emoji in address accepted
		{
			name: "bug 18: emoji in address accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x527F3692F5C5😀CfA83F7689885995606F93b6164",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// BUG 19: Decimal numbers instead of hex accepted
		{
			name: "bug 19: decimal numbers instead of hex accepted",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0x123456789012345678901234567890123456789",
				},
			},
			expectError: true, // SHOULD fail but current implementation accepts it
		},
		// Valid test case for a real contract address
		{
			name: "real contract address (USDC)",
			msg: &types.MsgUpdateAdminParams{
				Admin: adminAddr.String(),
				AdminParams: types.AdminParams{
					FactoryAddress: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
				},
			},
			expectError: false, // This should pass validation
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.msgServer.UpdateAdminParams(f.ctx, tc.msg)

			if tc.expectError {
				require.Error(err)
				if tc.errorContains != "" {
					require.Contains(err.Error(), tc.errorContains)
				}
			} else {
				require.NoError(err)
				// Response is nil for this message
				// require.NotNil(resp)

				// Verify params were actually set
				adminParams, err := f.k.AdminParams.Get(f.ctx)
				require.NoError(err)

				// If it's a success case with a valid address, verify it was set correctly
				if !strings.HasPrefix(tc.name, "bug") {
					require.Equal(tc.msg.AdminParams.FactoryAddress, adminParams.FactoryAddress)
				}
			}
		})
	}
}
