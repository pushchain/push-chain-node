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
				Signer:               validAdmin,
				CoreValidatorAddress: validCoreVal,
				Pubkey:               "pubkey_temp",
				Network: &types.NetworkInfo{
					Ip: "ip",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgAddUniversalValidator{
				Signer:               "bad_signer",
				CoreValidatorAddress: validCoreVal,
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "invalid core validator address (format)",
			msg: types.MsgAddUniversalValidator{
				Signer:               validAdmin,
				CoreValidatorAddress: "not_a_valoper",
			},
			wantErr: true,
			errMsg:  "invalid core validator address",
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
