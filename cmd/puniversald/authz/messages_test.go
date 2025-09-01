package authz

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	
	uetypes "github.com/rollchains/pchain/x/ue/types"
)

func init() {
	sdkConfig := sdk.GetConfig()
	defer func() {
		// Config already sealed, that's fine - ignore panic
		_ = recover()
	}()
	sdkConfig.SetBech32PrefixForAccount("push", "pushpub")
	sdkConfig.SetBech32PrefixForValidator("pushvaloper", "pushvaloperpub")
	sdkConfig.SetBech32PrefixForConsensusNode("pushvalcons", "pushvalconspub")
}

func TestParseMsgVoteInbound(t *testing.T) {
	tests := []struct {
		name      string
		msgType   string
		msgArgs   []string
		wantErr   bool
		errMsg    string
		validate  func(t *testing.T, msg interface{})
	}{
		{
			name:    "valid MsgVoteInbound",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20", // signer - valid bech32 address
				"eip155:11155111", // source chain
				"0x123abc", // tx hash
				"0xsender", // sender
				"0xrecipient", // recipient
				"1000", // amount
				"0xasset", // asset addr
				"1", // log index
				"1", // tx type (SYNTHETIC)
			},
			wantErr: false,
			validate: func(t *testing.T, msg interface{}) {
				voteMsg, ok := msg.(*uetypes.MsgVoteInbound)
				require.True(t, ok)
				assert.Equal(t, "push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20", voteMsg.Signer)
				assert.Equal(t, "eip155:11155111", voteMsg.Inbound.SourceChain)
				assert.Equal(t, "0x123abc", voteMsg.Inbound.TxHash)
				assert.Equal(t, "0xsender", voteMsg.Inbound.Sender)
				assert.Equal(t, "0xrecipient", voteMsg.Inbound.Recipient)
				assert.Equal(t, "1000", voteMsg.Inbound.Amount)
				assert.Equal(t, "0xasset", voteMsg.Inbound.AssetAddr)
				assert.Equal(t, "1", voteMsg.Inbound.LogIndex)
				assert.Equal(t, uetypes.InboundTxType_SYNTHETIC, voteMsg.Inbound.TxType)
			},
		},
		{
			name:    "MsgVoteInbound with FEE_ABSTRACTION type",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
				"eip155:1",
				"0xdef456",
				"0xsender2",
				"0xrecipient2",
				"5000",
				"0xtoken",
				"5",
				"2", // FEE_ABSTRACTION
			},
			wantErr: false,
			validate: func(t *testing.T, msg interface{}) {
				voteMsg, ok := msg.(*uetypes.MsgVoteInbound)
				require.True(t, ok)
				assert.Equal(t, uetypes.InboundTxType_FEE_ABSTRACTION, voteMsg.Inbound.TxType)
			},
		},
		{
			name:    "MsgVoteInbound with UNSPECIFIED type",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
				"eip155:1",
				"0xdef456",
				"0xsender2",
				"0xrecipient2",
				"5000",
				"0xtoken",
				"5",
				"0", // UNSPECIFIED
			},
			wantErr: false,
			validate: func(t *testing.T, msg interface{}) {
				voteMsg, ok := msg.(*uetypes.MsgVoteInbound)
				require.True(t, ok)
				assert.Equal(t, uetypes.InboundTxType_UNSPECIFIED_TX, voteMsg.Inbound.TxType)
			},
		},
		{
			name:    "MsgVoteInbound with insufficient args",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{"push1abc123", "eip155:1"},
			wantErr: true,
			errMsg:  "MsgVoteInbound requires",
		},
		{
			name:    "MsgVoteInbound with invalid signer",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"invalid_address",
				"eip155:1",
				"0x123",
				"0xsender",
				"0xrecipient",
				"1000",
				"0xasset",
				"1",
				"1",
			},
			wantErr: true,
			errMsg:  "invalid signer address",
		},
		{
			name:    "MsgVoteInbound with invalid tx type",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
				"eip155:1",
				"0x123",
				"0xsender",
				"0xrecipient",
				"1000",
				"0xasset",
				"1",
				"5", // Invalid type
			},
			wantErr: true,
			errMsg:  "invalid tx type 5",
		},
		{
			name:    "MsgVoteInbound with non-numeric tx type",
			msgType: "/ue.v1.MsgVoteInbound",
			msgArgs: []string{
				"push1gjaw568e35hjc8udhat0xnsxxmkm2snrexxz20",
				"eip155:1",
				"0x123",
				"0xsender",
				"0xrecipient",
				"1000",
				"0xasset",
				"1",
				"abc", // Non-numeric
			},
			wantErr: true,
			errMsg:  "invalid tx type (must be number",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := ParseMessageFromArgs(tt.msgType, tt.msgArgs)
			
			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, msg)
				if tt.validate != nil {
					tt.validate(t, msg)
				}
			}
		})
	}
}