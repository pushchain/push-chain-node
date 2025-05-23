package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAdminParamsValidate(t *testing.T) {
	// IMPORTANT: This test documents how validation SHOULD work, not how it currently works.
	// Most of these test cases will fail with the current implementation,
	// showing the bugs in the address validation logic.
	tests := []struct {
		name         string
		factoryAddr  string
		expectError  bool
		errorMessage string
	}{
		{
			name:        "valid address",
			factoryAddr: "0x527F3692F5C53CfA83F7689885995606F93b6164",
			expectError: false,
		},
		{
			name:         "empty address",
			factoryAddr:  "",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "invalid hex characters",
			factoryAddr:  "0xZZZF3692F5C53CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "zero address",
			factoryAddr:  "0x0000000000000000000000000000000000000000",
			expectError:  true,
			errorMessage: "zero address not allowed",
		},
		{
			name:         "missing 0x prefix",
			factoryAddr:  "527F3692F5C53CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "must start with 0x",
		},
		{
			name:         "wrong length",
			factoryAddr:  "0x527F3692F5C53CfA83F7689885995606F93b616400",
			expectError:  true,
			errorMessage: "incorrect length",
		},
		{
			name:         "too short",
			factoryAddr:  "0x527",
			expectError:  true,
			errorMessage: "invalid factory address",
		},
		{
			name:         "URL format",
			factoryAddr:  "http://malicious.com",
			expectError:  true,
			errorMessage: "invalid factory address",
		},
		{
			name:         "control characters",
			factoryAddr:  "0x527F3692F5C53\nCfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "contains control characters",
		},
		{
			name:         "precompiled address",
			factoryAddr:  "0x0000000000000000000000000000000000000001",
			expectError:  true,
			errorMessage: "invalid factory address",
		},
		{
			name:         "cosmos address",
			factoryAddr:  "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			expectError:  true,
			errorMessage: "not an EVM address",
		},
		{
			name:         "uppercase X prefix",
			factoryAddr:  "0X527F3692F5C53CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "must start with 0x",
		},
		{
			name:         "non-checksummed address",
			factoryAddr:  "0x527f3692f5c53cfa83f7689885995606f93b6164",
			expectError:  true,
			errorMessage: "invalid checksum",
		},
		{
			name:         "address with whitespace",
			factoryAddr:  "0x527F3692F5C5 3CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "contains whitespace",
		},
		{
			name:         "javascript injection attempt",
			factoryAddr:  "0x<script>alert(1)</script>",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "sql injection attempt",
			factoryAddr:  "0x'; DROP TABLE params; --",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "special characters in address",
			factoryAddr:  "0x527F3692F5C5$CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "emoji in address",
			factoryAddr:  "0x527F3692F5C5😀CfA83F7689885995606F93b6164",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:         "decimal numbers instead of hex",
			factoryAddr:  "0x123456789012345678901234567890123456789",
			expectError:  true,
			errorMessage: "invalid address",
		},
		{
			name:        "real contract address (USDC)",
			factoryAddr: "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48",
			expectError: false,
		},
	}

	// This test demonstrates how validation SHOULD work
	// Current implementation will fail these assertions
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := AdminParams{
				FactoryAddress: tt.factoryAddr,
			}

			err := params.ValidateBasic()

			if tt.expectError {
				require.Error(t, err)
				if tt.errorMessage != "" {
					require.Contains(t, err.Error(), tt.errorMessage)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
