package types_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

func TestIsPC20Payload(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{"selector only", "0x50433230", true},
		{"selector + metadata", "0x50433230000000000000000000000000dac17f958d2ee523a2206206994597c13d831ec7", true},
		{"selector uppercase hex", "0x50433230ABCD", true},
		{"no 0x prefix", "50433230deadbeef", true},
		{"with surrounding space", "  0x50433230  ", true},
		{"empty", "", false},
		{"just 0x", "0x", false},
		{"too short", "0x504332", false},
		{"prc20 funds-only (empty-ish)", "0x", false},
		{"prc20 abi-tuple offset", "0x0000002000000000000000000000000000000000000000000000000000000000", false},
		{"different selector", "0x50524332", false}, // "PRC2"-ish, not PC20
		{"garbage", "0xdeadbeef", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, types.IsPC20Payload(tc.payload))
		})
	}
}
