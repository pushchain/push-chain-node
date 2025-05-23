package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsgExecutePayload_ValidateBasic(t *testing.T) {
	validAccountId := &AccountId{
		Namespace: "ethereum",
		ChainId:   "1",
		OwnerKey:  "0x30ea71869947818d27b718592ea44010b458903bd9bf0370f50eda79e87d9f69",
		VmType:    VM_TYPE_EVM,
	}

	validPayload := &CrossChainPayload{
		Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
		Value:                "0",
		Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "1",
		Deadline:             "9999999999",
	}

	tests := []struct {
		name              string
		signer            string
		accountId         *AccountId
		crosschainPayload *CrossChainPayload
		signature         string
		expectError       bool
		errorContains     string
	}{
		{
			name:              "valid execute payload message",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       false,
		},
		{
			name:              "empty signer",
			signer:            "",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       true,
			errorContains:     "invalid signer",
		},
		{
			name:              "nil account id",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         nil,
			crosschainPayload: validPayload,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       true,
			errorContains:     "accountId cannot be nil",
		},
		{
			name:              "nil crosschain payload",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: nil,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       true,
			errorContains:     "crosschain payload cannot be nil",
		},
		{
			name:              "empty signature",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "",
			expectError:       true,
			errorContains:     "signature cannot be empty",
		},
		{
			name:      "invalid target address in payload",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0xZZZF3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:     "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:   true,
			errorContains: "invalid target address",
		},
		{
			name:      "zero target address in payload",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x0000000000000000000000000000000000000000",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should zero address be allowed? Worth testing
		},
		{
			name:      "invalid hex data in payload",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0xZZZZed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:     "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:   true,
			errorContains: "invalid hex data",
		},
		{
			name:              "invalid signature hex",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "0xZZZd4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       true,
			errorContains:     "invalid signature hex",
		},
		{
			name:              "signature without 0x prefix",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       false, // Should 0x prefix be required? Worth testing
		},
		{
			name:      "extremely high gas limit",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "999999999999999999999999999999",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should gas limits be validated? Worth testing
		},
		{
			name:      "extremely high value transfer",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "999999999999999999999999999999999999999999999",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should value limits be validated? Worth testing
		},
		{
			name:      "deadline in the past",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "1", // Very old timestamp
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should deadline be validated against current time? Worth testing
		},
		{
			name:      "nonce zero",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "0",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should nonce zero be allowed? Worth testing
		},
		{
			name:      "maxPriorityFeePerGas higher than maxFeePerGas",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "2000000000", // Higher than max fee
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Should fee validation be implemented? Worth testing
		},
		{
			name:      "empty data field",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "1000000000000000000", // 1 ETH
				Data:                 "",                    // Empty data for simple transfer
				GasLimit:             "21000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false,
		},
		{
			name:      "signature replay attack simulation",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed980000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:   "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError: false, // Same signature as first test - should replay protection exist? Worth testing
		},
		{
			name:              "short signature",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb8131",
			expectError:       false, // Should signature length be validated? Worth testing
		},
		{
			name:              "extremely long signature",
			signer:            "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId:         validAccountId,
			crosschainPayload: validPayload,
			signature:         "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:       false, // Should signature length be limited? Worth testing
		},
		{
			name:      "malformed data with control characters",
			signer:    "cosmos1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
			accountId: validAccountId,
			crosschainPayload: &CrossChainPayload{
				Target:               "0x527F3692F5C53CfA83F7689885995606F93b6164",
				Value:                "0",
				Data:                 "0x2ba2ed98\n0000000000000000000000000000000000000000000000000000000000000312",
				GasLimit:             "21000000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "200000000",
				Nonce:                "1",
				Deadline:             "9999999999",
			},
			signature:     "0x911d4ee13db2ca041e52c0e77035e4c7c82705a77e59368740ef42edcdb813144aff65d2a3a6d03215f764a037a229170c69ffbaaad50fff690940a5ef458304",
			expectError:   true,
			errorContains: "invalid hex data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &MsgExecutePayload{
				Signer:            tt.signer,
				AccountId:         tt.accountId,
				CrosschainPayload: tt.crosschainPayload,
				Signature:         tt.signature,
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
