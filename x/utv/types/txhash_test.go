package types_test

import (
	"testing"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utvtypes "github.com/pushchain/push-chain-node/x/utv/types"
	"github.com/stretchr/testify/require"
)

func TestNormalizeTxHash_EVM(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "valid hex with 0x prefix",
			input:    "0x65586d3dca140f89b5ebe7103b9b4da55c463e88132dd1f5f0ffe4cd4e11a35d",
			expected: "0x65586d3dca140f89b5ebe7103b9b4da55c463e88132dd1f5f0ffe4cd4e11a35d",
		},
		{
			name:     "valid hex without 0x prefix and uppercase",
			input:    "65586d3dca140f89b5ebe7103b9b4da55c463e88132dd1f5f0ffe4cd4e11a35d",
			expected: "0x65586d3dca140f89b5ebe7103b9b4da55c463e88132dd1f5f0ffe4cd4e11a35d",
		},
		{
			name:    "invalid hex string",
			input:   "0xxyz123",
			wantErr: true,
		},
		{
			name:    "too short hex",
			input:   "0x123",
			wantErr: true,
		},
		{
			name:    "not hex at all",
			input:   "hello_world",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := utvtypes.NormalizeTxHash(tc.input, uexecutortypes.VM_TYPE_EVM)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, res)
			}
		})
	}
}

func TestNormalizeTxHash_SVM(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
		wantErr  bool
	}{
		{
			name:     "valid base58 (64 bytes)",
			input:    "0x46c673d6dda4695f38495e7863875687b40f9c56591bc2013c9907605244067bc11f50b0680a9379c37f57725a87b6568943eee30d9505e116f3ec5089787000",
			expected: "2R58yZhQiitzU33ACpeikF4XdRpRfxxoBHviFhZV5ABM99a8h6gmDGWkE4VgJfBsivfp2znhNAmVNkhsqL9s3Hod",
			wantErr:  false,
		},
		{
			name:    "too short base58 string",
			input:   "123abcXYZ",
			wantErr: true,
		},
		{
			name:    "non-base58 characters",
			input:   "!@#^&*(",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := utvtypes.NormalizeTxHash(tc.input, uexecutortypes.VM_TYPE_SVM)
			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.expected, res)
			}
		})
	}
}
