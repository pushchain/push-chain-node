package types_test

import (
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/utss/types"
)

const validSigner = "push1fgaewhyd9fkwtqaj9c233letwcuey6dgly9gv9"

func baseInitiateMsg() *types.MsgInitiateFundMigration {
	return &types.MsgInitiateFundMigration{
		Signer:   validSigner,
		OldKeyId: "old-key-1",
		Chain:    "eip155:11155111",
		Balance:  "1000000000000000000",
	}
}

func TestMsgInitiateFundMigration_ValidateBasic(t *testing.T) {
	maxUint256 := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))

	tests := []struct {
		name    string
		mutate  func(*types.MsgInitiateFundMigration)
		wantErr string
	}{
		{"valid", func(m *types.MsgInitiateFundMigration) {}, ""},
		{"max uint256 balance accepted", func(m *types.MsgInitiateFundMigration) {
			m.Balance = maxUint256.String()
		}, ""},
		{"zero balance accepted here (the keeper rejects it once fees are known)", func(m *types.MsgInitiateFundMigration) {
			m.Balance = "0"
		}, ""},
		{"bad signer", func(m *types.MsgInitiateFundMigration) { m.Signer = "not-bech32" }, "invalid signer"},
		{"missing old_key_id", func(m *types.MsgInitiateFundMigration) { m.OldKeyId = "  " }, "old_key_id is required"},
		{"missing chain", func(m *types.MsgInitiateFundMigration) { m.Chain = "" }, "chain is required"},
		{"missing balance", func(m *types.MsgInitiateFundMigration) { m.Balance = "" }, "balance is required"},
		{"negative balance", func(m *types.MsgInitiateFundMigration) { m.Balance = "-1" }, "non-negative"},
		{"non-numeric balance", func(m *types.MsgInitiateFundMigration) { m.Balance = "1e18" }, "non-negative"},
		{"balance over uint256", func(m *types.MsgInitiateFundMigration) {
			m.Balance = new(big.Int).Add(maxUint256, big.NewInt(1)).String()
		}, "exceeds uint256"},
		{
			// 78 nines fits the 80-character cap but is wider than uint256, so a
			// length check alone would let it through. The EVM ABI encoder
			// truncates such values silently, so BitLen is the load-bearing check.
			"78 nines rejected despite fitting the length cap",
			func(m *types.MsgInitiateFundMigration) { m.Balance = strings.Repeat("9", 78) },
			"exceeds uint256",
		},
		{"absurdly long balance", func(m *types.MsgInitiateFundMigration) {
			m.Balance = strings.Repeat("9", 1000)
		}, "too long"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg := baseInitiateMsg()
			tc.mutate(msg)
			err := msg.ValidateBasic()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestMsgInitiateFundMigration_RejectsHugeBalanceFast pins the ordering inside
// ParseBalance: the length cap has to run before big.Int parses the string.
// Decimal parsing is superlinear, so without the cap a caller-supplied
// multi-million-digit balance is fully parsed and then thrown away.
func TestMsgInitiateFundMigration_RejectsHugeBalanceFast(t *testing.T) {
	msg := baseInitiateMsg()
	msg.Balance = strings.Repeat("9", 3_000_000)

	start := time.Now()
	err := msg.ValidateBasic()
	elapsed := time.Since(start)

	// Timing first: require.Contains aborts the test, so asserting the message
	// before the duration would mean the duration is never checked.
	require.Less(t, elapsed, time.Second,
		"rejecting a 3000000-digit balance took %s — the length cap must reject before big.Int parses", elapsed)
	require.Error(t, err)
	require.Contains(t, err.Error(), "too long")
}
