package types_test

import (
	"strings"
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func mockHexAddress() string {
	return "0x" + strings.Repeat("a1", 20)
}

func TestUniversalPayload_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		payload   types.UniversalPayload
		expectErr bool
		errType   string
	}{
		{
			name: "valid - minimal fields",
			payload: types.UniversalPayload{
				To: mockHexAddress(),
			},
			expectErr: false,
		},
		{
			name: "valid - full payload",
			payload: types.UniversalPayload{
				To:                   mockHexAddress(),
				Value:                "1000000000000000000",
				Data:                 "0xdeadbeef",
				GasLimit:             "21000",
				MaxFeePerGas:         "1000000000",
				MaxPriorityFeePerGas: "2000000000",
				Nonce:                "42",
				Deadline:             "9999999999",
				VType:                types.VerificationType_universalTxVerification,
			},
			expectErr: false,
		},
		{
			name: "invalid - empty to address",
			payload: types.UniversalPayload{
				To: "",
			},
			expectErr: true,
			errType:   "to address cannot be empty",
		},
		{
			name: "invalid - malformed to address",
			payload: types.UniversalPayload{
				To: "not-an-address",
			},
			expectErr: true,
			errType:   "invalid to address format",
		},
		{
			name: "invalid - data not hex",
			payload: types.UniversalPayload{
				To:   mockHexAddress(),
				Data: "nothex",
			},
			expectErr: true,
			errType:   "invalid hex data",
		},
		{
			name: "invalid - value not uint",
			payload: types.UniversalPayload{
				To:    mockHexAddress(),
				Value: "12.34",
			},
			expectErr: true,
			errType:   "value must be a valid unsigned integer",
		},
		{
			name: "invalid - gas_limit not uint",
			payload: types.UniversalPayload{
				To:       mockHexAddress(),
				GasLimit: "twenty-one-thousand",
			},
			expectErr: true,
			errType:   "gas_limit must be a valid unsigned integer",
		},
		{
			name: "invalid - max_fee_per_gas not uint",
			payload: types.UniversalPayload{
				To:           mockHexAddress(),
				MaxFeePerGas: "-1",
			},
			expectErr: true,
			errType:   "max_fee_per_gas must be a valid unsigned integer",
		},
		{
			name: "invalid - max_priority_fee_per_gas not uint",
			payload: types.UniversalPayload{
				To:                   mockHexAddress(),
				MaxPriorityFeePerGas: "1.5",
			},
			expectErr: true,
			errType:   "max_priority_fee_per_gas must be a valid unsigned integer",
		},
		{
			name: "invalid - nonce not uint",
			payload: types.UniversalPayload{
				To:    mockHexAddress(),
				Nonce: "abc",
			},
			expectErr: true,
			errType:   "nonce must be a valid unsigned integer",
		},
		{
			name: "invalid - deadline not uint",
			payload: types.UniversalPayload{
				To:       mockHexAddress(),
				Deadline: "time-is-up",
			},
			expectErr: true,
			errType:   "deadline must be a valid unsigned integer",
		},
		{
			name: "invalid - sig type out of enum range",
			payload: types.UniversalPayload{
				To:    mockHexAddress(),
				VType: 99,
			},
			expectErr: true,
			errType:   "invalid verificationData type",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.payload.ValidateBasic()

			if tc.expectErr {
				require.Error(t, err)
				if tc.errType != "" {
					require.Contains(t, err.Error(), tc.errType)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
