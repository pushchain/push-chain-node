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

	validSigner := "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20"

	tests := []struct {
		name    string
		msg     types.MsgUpdateUniversalValidator
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid message",
			msg: types.MsgUpdateUniversalValidator{
				Signer: validSigner,
				Network: &types.NetworkInfo{
					PeerId:     "temp peerId",
					MultiAddrs: []string{"temp multi_addrs"},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid signer address",
			msg: types.MsgUpdateUniversalValidator{
				Signer: "invalid_signer",
				Network: &types.NetworkInfo{
					PeerId:     "temp peerId",
					MultiAddrs: []string{"temp multi_addrs"},
				},
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name: "empty peerId should fail",
			msg: types.MsgUpdateUniversalValidator{
				Signer:  validSigner,
				Network: &types.NetworkInfo{PeerId: "", MultiAddrs: []string{"temp multi_addrs"}},
			},
			wantErr: true,
			errMsg:  "peerId cannot be empty",
		},
		{
			name: "nil multi_addrs in networkInfo should fail",
			msg: types.MsgUpdateUniversalValidator{
				Signer:  validSigner,
				Network: &types.NetworkInfo{PeerId: "temp peerId", MultiAddrs: nil},
			},
			wantErr: true,
			errMsg:  "multi_addrs cannot be nil",
		},
		{
			name: "empty multi_addrs in networkInfo should fail",
			msg: types.MsgUpdateUniversalValidator{
				Signer:  validSigner,
				Network: &types.NetworkInfo{PeerId: "temp peerId", MultiAddrs: []string{}},
			},
			wantErr: true,
			errMsg:  "multi_addrs must contain at least one value",
		},
		{
			// F-2026-16994: nil Network used to panic — value-receiver
			// ValidateBasic call through a nil *NetworkInfo.
			name: "nil network returns typed error, no panic",
			msg: types.MsgUpdateUniversalValidator{
				Signer:  validSigner,
				Network: nil,
			},
			wantErr: true,
			errMsg:  "network info is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.NotPanics(t, func() {
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
		})
	}
}
