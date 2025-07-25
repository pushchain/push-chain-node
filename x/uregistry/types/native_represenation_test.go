package types_test

import (
	"testing"

	"github.com/rollchains/pchain/x/uregistry/types"
	"github.com/stretchr/testify/require"
)

func TestNativeRepresentation_ValidateBasic(t *testing.T) {
	tests := []struct {
		name      string
		nativeRep types.NativeRepresentation
		expectErr bool
		errMsg    string
	}{
		{
			name: "valid - both fields empty (optional)",
			nativeRep: types.NativeRepresentation{
				Denom:           "",
				ContractAddress: "",
			},
			expectErr: false,
		},
		{
			name: "valid - only denom set",
			nativeRep: types.NativeRepresentation{
				Denom:           "uatom",
				ContractAddress: "",
			},
			expectErr: false,
		},
		{
			name: "valid - only contract_address set with 0x",
			nativeRep: types.NativeRepresentation{
				Denom:           "",
				ContractAddress: "0xabc123def4567890",
			},
			expectErr: false,
		},
		{
			name: "valid - both denom and contract_address set",
			nativeRep: types.NativeRepresentation{
				Denom:           "uatom",
				ContractAddress: "0xdeadbeefcafebabe",
			},
			expectErr: false,
		},
		{
			name: "invalid - contract_address without 0x",
			nativeRep: types.NativeRepresentation{
				Denom:           "uatom",
				ContractAddress: "abc123",
			},
			expectErr: true,
			errMsg:    "contract_address must start with 0x",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.nativeRep.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
