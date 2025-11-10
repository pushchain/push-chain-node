package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestMsgUpdateUniversalValidator_ValidateBasic(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)

	validAdmin := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
	validCoreVal := "pushvaloper1gjaw568e35hjc8udhat0xnsxxmkm2snrjnakhg"

	tests := []struct {
		name    string
		msg     types.MsgUpdateUniversalValidator
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: types.MsgUpdateUniversalValidator{
				Signer:               validAdmin,
				CoreValidatorAddress: validCoreVal,
				Pubkey:               "updated_pubkey_123",
				Network: &types.NetworkInfo{
					Ip: "127.0.0.1",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgUpdateUniversalValidator{
				Signer:               "invalid_signer",
				CoreValidatorAddress: validCoreVal,
				Pubkey:               "pubkey_temp",
				Network: &types.NetworkInfo{
					Ip: "10.0.0.1",
				},
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "invalid core validator address format",
			msg: types.MsgUpdateUniversalValidator{
				Signer:               validAdmin,
				CoreValidatorAddress: "bad_valoper_format",
				Pubkey:               "pubkey_temp",
				Network: &types.NetworkInfo{
					Ip: "10.0.0.1",
				},
			},
			wantErr: true,
			errMsg:  "invalid core validator address",
		},
		{
			name: "empty pubkey should fail",
			msg: types.MsgUpdateUniversalValidator{
				Signer:               validAdmin,
				CoreValidatorAddress: validCoreVal,
				Pubkey:               "   ",
				Network: &types.NetworkInfo{
					Ip: "10.0.0.1",
				},
			},
			wantErr: true,
			errMsg:  "pubkey cannot be empty",
		},
		{
			name: "empty network info should fail",
			msg: types.MsgUpdateUniversalValidator{
				Signer:               validAdmin,
				CoreValidatorAddress: validCoreVal,
				Pubkey:               "valid_pubkey",
				Network:              &types.NetworkInfo{Ip: ""},
			},
			wantErr: true,
			errMsg:  "ip cannot be empty",
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
