package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgUpdateParams_ValidateBasic(t *testing.T) {
	// Test cases for MsgUpdateParams validation
	tests := []struct {
		name          string
		authority     string
		params        *Params
		expectError   bool
		errorContains string
	}{
		{
			name:      "valid governance authority",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			},
			expectError: false,
		},
		{
			name:          "empty authority",
			authority:     "",
			params:        &Params{Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"},
			expectError:   true,
			errorContains: "invalid authority",
		},
		{
			name:          "invalid authority format",
			authority:     "invalid-authority",
			params:        &Params{Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"},
			expectError:   true,
			errorContains: "invalid authority",
		},
		{
			name:          "authority with special characters",
			authority:     "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn; DROP TABLE;",
			params:        &Params{Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"},
			expectError:   true,
			errorContains: "invalid authority",
		},
		{
			name:          "nil params",
			authority:     "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params:        nil,
			expectError:   true,
			errorContains: "params cannot be nil",
		},
		{
			name:      "empty admin in params",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "",
			},
			expectError:   true,
			errorContains: "admin cannot be empty",
		},
		{
			name:      "invalid admin address format",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "invalid-admin-address",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "hex address as admin (should fail)",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "0x527F3692F5C53CfA83F7689885995606F93b6164",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "admin with control characters",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat0xns\nxxmkm2snrexxz20",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "admin with unicode characters",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat0xns😀xxmkm2snrexxz20",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "extremely long admin address",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "admin with SQL injection attempt",
			authority: "cosmos10d07y265gmmuvt4z0w9aw880jnsr700j6zn9kn",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat'; DROP TABLE admins; --",
			},
			expectError:   true,
			errorContains: "invalid admin address",
		},
		{
			name:      "authority same as admin (potential centralization)",
			authority: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			params: &Params{
				Admin: "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			},
			expectError: false, // This might be allowed but worth testing
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &MsgUpdateParams{
				Authority: tt.authority,
				Params:    *tt.params,
			}

			err := msg.ValidateBasic()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					require.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
