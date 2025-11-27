package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestMsgMigrateUEA_ValidateBasic(t *testing.T) {
	validSigner := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

	validUA := &types.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	validMigrationPayload := &types.MigrationPayload{
		Migration: "0x000000000000000000000000000000000000dead",
		Nonce:     "1",
		Deadline:  "9999999999",
	}

	invalidMigrationPayload := &types.MigrationPayload{
		Migration: "bad_address",
		Nonce:     "1",
		Deadline:  "1",
	}

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
				MigrationPayload:   validMigrationPayload,
				Signature:          "0xabcdef",
			},
			expectErr: false,
		},
		{
			name: "fails when migration payload validation fails (delegation)",
			msg: &types.MsgMigrateUEA{
				Signer:             validSigner,
				UniversalAccountId: validUA,
				MigrationPayload:   invalidMigrationPayload,
				Signature:          "0xabcdef",
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
