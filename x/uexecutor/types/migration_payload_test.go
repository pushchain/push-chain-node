package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestMsgMigrationPayload_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"
	invalidSigner := "invalid_bech32"
	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	validPayload := &types.MigrationPayload{
		Migration: "0x000000000000000000000000000000000000dead",
	}

	invalidPayload := &types.MigrationPayload{
		Migration: "invalid_address",
	}

	validSig := "abcdef0123456789"
	// invalidSig := "zzzzzz"

	tests := []struct {
		name      string
		msg       *types.MsgMigrateUEA
		expectErr bool
	}{
		{
			name: "valid msg",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   validPayload,
				Signature:          validSig,
			},
			expectErr: false,
		},
		{
			name: "invalid signer",
			msg: &types.MsgMigrateUEA{
				Signer:             invalidSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   validPayload,
				Signature:          validSig,
			},
			expectErr: true,
		},
		{
			name: "nil universal account",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: nil,
				MigrationPayload:   validPayload,
				Signature:          validSig,
			},
			expectErr: true,
		},
		{
			name: "nil universal payload",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   nil,
				Signature:          validSig,
			},
			expectErr: true,
		},
		{
			name: "empty Signature",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   validPayload,
				Signature:          "",
			},
			expectErr: true,
		},
		{
			name: "invalid universal payload data",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   invalidPayload,
				Signature:          validSig,
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
