package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestMsgAddUniversalValidator_ValidateBasic(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)

	// Valid test addresses
	validAdmin := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"
	validAccount := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"        // a valid account address
	validCoreVal := "pushvaloper1gjaw568e35hjc8udhat0xnsxxmkm2snrjnakhg" // a valid valoper address

	tests := []struct {
		name    string
		msg     types.MsgAddUniversalValidator
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: types.MsgAddUniversalValidator{
				Signer:                    validAdmin,
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: validAccount,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgAddUniversalValidator{
				Signer:                    "bad_signer",
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: validAccount,
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "invalid core validator address (format)",
			msg: types.MsgAddUniversalValidator{
				Signer:                    validAdmin,
				CoreValidatorAddress:      "not_a_valoper",
				UniversalValidatorAddress: validAccount,
			},
			wantErr: true,
			errMsg:  "invalid core validator address",
		},
		{
			name: "invalid universal validator address",
			msg: types.MsgAddUniversalValidator{
				Signer:                    validAdmin,
				CoreValidatorAddress:      validCoreVal,
				UniversalValidatorAddress: "nope",
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
