package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rollchains/pchain/app"
	"github.com/rollchains/pchain/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestMsgUpdateUniversalValidator_ValidateBasic(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)

	validAdmin := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
	validAccount := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
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
				Signer:                    validAdmin,
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: validAccount,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgUpdateUniversalValidator{
				Signer:                    "bad_signer",
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: validAccount,
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "invalid core validator address",
			msg: types.MsgUpdateUniversalValidator{
				Signer:                    validAdmin,
				CoreValidatorAddress:      "not_a_valoper",
				UniversalValidatorAddress: validAccount,
			},
			wantErr: true,
			errMsg:  "invalid core validator address",
		},
		{
			name: "invalid universal validator address",
			msg: types.MsgUpdateUniversalValidator{
				Signer:                    validAdmin,
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: "not_an_account",
			},
			wantErr: true,
			errMsg:  "invalid universal validator address",
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
