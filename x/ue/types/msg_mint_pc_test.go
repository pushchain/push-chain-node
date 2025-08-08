package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestMsgMintPC_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "not_bech32"

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
		name      string
		msg       *types.MsgMintPC
		expectErr bool
	}{
		{
			name: "valid message",
			msg: &types.MsgMintPC{
				Signer:             validSigner,
				UniversalAccountId: validUAcc,
				TxHash:             "0x123abc",
			},
			expectErr: false,
		},
		{
			name: "invalid signer address",
			msg: &types.MsgMintPC{
				Signer:             invalidSigner,
				UniversalAccountId: validUAcc,
				TxHash:             "0x123abc",
			},
			expectErr: true,
		},
		{
			name: "nil universal account",
			msg: &types.MsgMintPC{
				Signer:             validSigner,
				UniversalAccountId: nil,
				TxHash:             "0x123abc",
			},
			expectErr: true,
		},
		{
			name: "invalid universal account",
			msg: &types.MsgMintPC{
				Signer:             validSigner,
				UniversalAccountId: invalidUAcc,
				TxHash:             "0x123abc",
			},
			expectErr: true,
		},
		{
			name: "empty tx hash",
			msg: &types.MsgMintPC{
				Signer:             validSigner,
				UniversalAccountId: validUAcc,
				TxHash:             "",
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
