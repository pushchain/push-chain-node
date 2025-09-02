package types_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

func TestMsgRemoveUniversalValidator_ValidateBasic(t *testing.T) {
	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(app.Bech32PrefixAccAddr, app.Bech32PrefixAccPub)
	cfg.SetBech32PrefixForValidator(app.Bech32PrefixValAddr, app.Bech32PrefixValPub)

	validAdmin := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"        // valid bech32 account address
	validUniversalVal := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20" // now treated as universal validator address

	tests := []struct {
		name    string
		msg     types.MsgRemoveUniversalValidator
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: types.MsgRemoveUniversalValidator{
				Signer:                    validAdmin,
				UniversalValidatorAddress: validUniversalVal,
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgRemoveUniversalValidator{
				Signer:                    "bad_signer",
				UniversalValidatorAddress: validUniversalVal,
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "invalid universal validator address",
			msg: types.MsgRemoveUniversalValidator{
				Signer:                    validAdmin,
				UniversalValidatorAddress: "not_a_valid_addr",
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
