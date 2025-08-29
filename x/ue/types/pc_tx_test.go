package types_test

import (
	"testing"

	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/rollchains/pchain/x/ue/types"
	"github.com/stretchr/testify/require"
)

func TestPCTx_ValidateBasic(t *testing.T) {
	tests := []struct {
		name    string
		tx      types.PCTx
		wantErr error
	}{
		{
			name: "valid SUCCESS tx",
			tx: types.PCTx{
				TxHash:      "0xabc123",
				Sender:      "0x1234567890abcdef1234567890abcdef12345678", // valid hex addr
				BlockHeight: 100,
				Status:      "SUCCESS",
			},
			wantErr: nil,
		},
		{
			name: "valid FAILED tx",
			tx: types.PCTx{
				TxHash:      "0xdef456",
				Sender:      "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd",
				BlockHeight: 200,
				Status:      "FAILED",
			},
			wantErr: nil,
		},
		{
			name: "empty tx_hash",
			tx: types.PCTx{
				TxHash:      "",
				Sender:      "0x1234567890abcdef1234567890abcdef12345678",
				BlockHeight: 100,
				Status:      "SUCCESS",
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "empty sender",
			tx: types.PCTx{
				TxHash:      "0xabc123",
				Sender:      "",
				BlockHeight: 100,
				Status:      "SUCCESS",
			},
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "invalid sender format",
			tx: types.PCTx{
				TxHash:      "0xabc123",
				Sender:      "invalid_address",
				BlockHeight: 100,
				Status:      "SUCCESS",
			},
			wantErr: sdkerrors.ErrInvalidAddress,
		},
		{
			name: "zero block_height",
			tx: types.PCTx{
				TxHash:      "0xabc123",
				Sender:      "0x1234567890abcdef1234567890abcdef12345678",
				BlockHeight: 0,
				Status:      "SUCCESS",
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
		{
			name: "invalid status",
			tx: types.PCTx{
				TxHash:      "0xabc123",
				Sender:      "0x1234567890abcdef1234567890abcdef12345678",
				BlockHeight: 100,
				Status:      "PENDING",
			},
			wantErr: sdkerrors.ErrInvalidRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.tx.ValidateBasic()
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}
