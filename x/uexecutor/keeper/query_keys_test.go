package keeper_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// solanaSig is a real 88-char base58 Solana signature (64 bytes).
const solanaSig = "5j7s6NiJS3JAkvgkoc18WVAsiSaci2pxB2A6ueCJP4tprA2TFg9wSyTLeYouxPBJEMzJinENTkpA52YStRW5Dia7"

func TestInboundKeys_RejectsOversizedTxHash(t *testing.T) {
	// F-2026-18821: InboundKeys is unauthenticated, reads no state and so burns
	// no gas. It canonicalizes tx_hash three times (Canonicalize, then the UTX
	// and ballot key helpers), and base58 decoding is quadratic — a 1e5-char
	// hash cost tens of seconds of CPU per request before the fix.
	f := SetupTest(t)

	huge := strings.Repeat("z", 100_000)

	start := time.Now()
	_, err := f.queryServer.InboundKeys(f.ctx, &types.QueryInboundKeysRequest{
		Inbound: &types.Inbound{
			SourceChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			TxHash:      huge,
			LogIndex:    "0",
			TxType:      types.TxType_FUNDS,
		},
	})
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
	require.Contains(t, err.Error(), "tx_hash too long")
	require.Less(t, elapsed, time.Second, "oversized tx_hash must fail fast (took %s)", elapsed)
}

func TestInboundKeys_AcceptsRealSolanaSignature(t *testing.T) {
	// The cap must not reject anything real: 88 chars is the longest a base58
	// 64-byte signature can be.
	f := SetupTest(t)

	resp, err := f.queryServer.InboundKeys(f.ctx, &types.QueryInboundKeysRequest{
		Inbound: &types.Inbound{
			SourceChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
			TxHash:      solanaSig,
			LogIndex:    "0",
			TxType:      types.TxType_FUNDS,
		},
	})

	require.NoError(t, err)
	require.NotEmpty(t, resp.UtxId)
	require.NotEmpty(t, resp.BallotId)
	// Canonicalization folds the base58 signature into 0x-hex.
	require.Equal(t, "0x", resp.CanonicalInbound.TxHash[:2])
	require.Len(t, resp.CanonicalInbound.TxHash, 2+128)
}

func TestInboundKeys_TxHashAtCapIsAccepted(t *testing.T) {
	// Boundary: exactly maxQueryTxHashLen (128) is allowed, 129 is not.
	f := SetupTest(t)

	newReq := func(n int) *types.QueryInboundKeysRequest {
		return &types.QueryInboundKeysRequest{
			Inbound: &types.Inbound{
				SourceChain: "solana:EtWTRABZaYq6iMfeYKouRu166VU2xqa1",
				TxHash:      strings.Repeat("z", n),
				LogIndex:    "0",
				TxType:      types.TxType_FUNDS,
			},
		}
	}

	_, err := f.queryServer.InboundKeys(f.ctx, newReq(128))
	require.NoError(t, err, "128-char tx_hash is at the cap and must be accepted")

	_, err = f.queryServer.InboundKeys(f.ctx, newReq(129))
	require.Error(t, err)
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
