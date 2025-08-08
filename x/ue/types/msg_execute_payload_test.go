package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestMsgExecutePayload_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "invalid_bech32"
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	validPayload := &types.UniversalPayload{
		To:   "0x000000000000000000000000000000000000dead",
		Data: "0xabcdef",
	}

	invalidPayload := &types.UniversalPayload{
		To:   "invalid_address",
		Data: "0xghijkl", // invalid hex
	}

	validSig := "abcdef0123456789"
	invalidSig := "zzzzzz"

	tests := []struct {
		name      string
		msg       *types.MsgExecutePayload
		expectErr bool
	}{
		{
			name: "valid msg",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   validPayload,
				VerificationData:   validSig,
			},
			expectErr: false,
		},
		{
			name: "invalid signer",
			msg: &types.MsgExecutePayload{
				Signer:             invalidSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   validPayload,
				VerificationData:   validSig,
			},
			expectErr: true,
		},
		{
			name: "nil universal account",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: nil,
				UniversalPayload:   validPayload,
				VerificationData:   validSig,
			},
			expectErr: true,
		},
		{
			name: "nil universal payload",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   nil,
				VerificationData:   validSig,
			},
			expectErr: true,
		},
		{
			name: "empty verificationData",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   validPayload,
				VerificationData:   "",
			},
			expectErr: true,
		},
		{
			name: "invalid verificationData hex",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   validPayload,
				VerificationData:   invalidSig,
			},
			expectErr: true,
		},
		{
			name: "invalid universal payload data",
			msg: &types.MsgExecutePayload{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				UniversalPayload:   invalidPayload,
				VerificationData:   validSig,
			},
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr {
				require.Error(t, err, "expected error but got none")
			} else {
				require.NoError(t, err, "expected no error but got: %v", err)
			}
		})
	}
}
