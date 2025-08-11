package types_test

import (
	"testing"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
	"github.com/stretchr/testify/require"
)

func TestMsgUpdateParams_ValidateBasic(t *testing.T) {
	validBech32 := "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

	tests := []struct {
		name    string
		msg     types.MsgUpdateParams
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: types.MsgUpdateParams{
				Authority: validBech32,
				Params: types.Params{
					Admin: validBech32,
				},
			},
			wantErr: false,
		},
		{
			name: "invalid authority address",
			msg: types.MsgUpdateParams{
				Authority: "invalid_bech32",
				Params: types.Params{
					Admin: validBech32,
				},
			},
			wantErr: true,
			errMsg:  "invalid authority address",
		},
		{
			name: "invalid admin address",
			msg: types.MsgUpdateParams{
				Authority: validBech32,
				Params: types.Params{
					Admin: "not_cosmos_address",
				},
			},
			wantErr: true,
			errMsg:  "invalid admin address",
		},
		{
			name: "empty admin address",
			msg: types.MsgUpdateParams{
				Authority: validBech32,
				Params: types.Params{
					Admin: "",
				},
			},
			wantErr: true,
			errMsg:  "invalid admin address",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()

			if tc.wantErr {
				require.Error(t, err)
				if tc.errMsg != "" {
					require.Contains(t, err.Error(), tc.errMsg)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
