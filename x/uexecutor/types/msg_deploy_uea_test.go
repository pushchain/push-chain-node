package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestMsgDeployUEA_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "invalid_bech32"
	validUAcc := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}
	invalidUAcc := &types.UniversalAccountId{
		ChainNamespace: "",
		ChainId:        "11155111",
		Owner:          "0xzzzzzzzz",
	}

	tests := []struct {
		name        string
		msg         *types.MsgDeployUEA
		expectError bool
		errContains string
	}{
		{
			name: "valid message",
			msg: types.NewMsgDeployUEA(
				sdk.MustAccAddressFromBech32(validSigner),
				validUAcc,
				"0xabc123",
			),
			expectError: false,
		},
		{
			name: "invalid signer format",
			msg: &types.MsgDeployUEA{
				Signer:             invalidSigner,
				UniversalAccountId: validUAcc,
				TxHash:             "0xabc123",
			},
			expectError: true,
			errContains: "invalid signer address",
		},
		{
			name: "nil universal account",
			msg: &types.MsgDeployUEA{
				Signer:             validSigner,
				UniversalAccountId: nil,
				TxHash:             "0xabc123",
			},
			expectError: true,
			errContains: "universalAccountId cannot be nil",
		},
		{
			name: "empty txHash",
			msg: &types.MsgDeployUEA{
				Signer:             validSigner,
				UniversalAccountId: validUAcc,
				TxHash:             "",
			},
			expectError: true,
			errContains: "txHash cannot be empty",
		},
		{
			name: "invalid universal account",
			msg: &types.MsgDeployUEA{
				Signer:             validSigner,
				UniversalAccountId: invalidUAcc,
				TxHash:             "0xabc123",
			},
			expectError: true,
			errContains: "chain namespace cannot be empty", // updated to reflect new validation
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
