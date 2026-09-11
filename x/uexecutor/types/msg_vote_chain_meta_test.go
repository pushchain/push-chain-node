package types_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// F-2026-18803 (stateless half): observed_chain_id becomes the ChainMetas map
// key verbatim, so ValidateBasic bounds its size and shape at CheckTx time.
func TestMsgVoteChainMeta_ValidateBasic(t *testing.T) {
	const validSigner = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

	newMsg := func(chainID string) *types.MsgVoteChainMeta {
		return &types.MsgVoteChainMeta{
			Signer:          validSigner,
			ObservedChainId: chainID,
			Price:           100_000_000_000,
			ChainHeight:     12345,
		}
	}

	tests := []struct {
		name      string
		msg       *types.MsgVoteChainMeta
		expectErr string
	}{
		{
			name: "valid evm chain id",
			msg:  newMsg("eip155:11155111"),
		},
		{
			name: "valid solana chain id",
			msg:  newMsg("solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1"),
		},
		{
			name: "chain id exactly at the cap is accepted",
			msg:  newMsg("eip155:" + strings.Repeat("9", types.MaxObservedChainIdLen-len("eip155:"))),
		},
		{
			name:      "chain id one byte over the cap is rejected",
			msg:       newMsg("eip155:" + strings.Repeat("9", types.MaxObservedChainIdLen-len("eip155:")+1)),
			expectErr: "exceeds 128 characters",
		},
		{
			name:      "oversized chain id is rejected",
			msg:       newMsg("eip155:" + strings.Repeat("9", 100_000)),
			expectErr: "exceeds 128 characters",
		},
		{
			name:      "non-CAIP-2 chain id is rejected",
			msg:       newMsg("ethereum"),
			expectErr: "CAIP-2 format",
		},
		{
			name:      "empty namespace is rejected",
			msg:       newMsg(":11155111"),
			expectErr: "CAIP-2 format",
		},
		{
			name:      "empty reference is rejected",
			msg:       newMsg("eip155:"),
			expectErr: "CAIP-2 format",
		},
		{
			name:      "empty chain id is rejected",
			msg:       newMsg(""),
			expectErr: "observed_chain_id cannot be empty",
		},
		{
			name:      "invalid signer is rejected",
			msg:       &types.MsgVoteChainMeta{Signer: "not-bech32", ObservedChainId: "eip155:1", Price: 1, ChainHeight: 1},
			expectErr: "invalid signer address",
		},
		{
			name:      "zero price is rejected",
			msg:       &types.MsgVoteChainMeta{Signer: validSigner, ObservedChainId: "eip155:1", Price: 0, ChainHeight: 1},
			expectErr: "price must be greater than 0",
		},
		{
			name:      "zero chain height is rejected",
			msg:       &types.MsgVoteChainMeta{Signer: validSigner, ObservedChainId: "eip155:1", Price: 1, ChainHeight: 0},
			expectErr: "chain_height must be greater than 0",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.msg.ValidateBasic()
			if tc.expectErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expectErr)
		})
	}
}
