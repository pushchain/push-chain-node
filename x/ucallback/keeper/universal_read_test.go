package keeper_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
)

func newRead(id, txHash string, expiry uint64, status types.UniversalReadStatus) types.UniversalRead {
	return types.UniversalRead{
		Id:     id,
		Status: status,
		Request: &types.ReadRequest{
			RequestId:         id,
			DestinationChain:  "eip155:1",
			ExpiryBlockHeight: expiry,
			RequestedTxHash:   txHash,
		},
	}
}

func TestSetUniversalRead_RoundTrips(t *testing.T) {
	f := SetupTest(t)

	require.False(t, f.k.HasUniversalRead(f.ctx, "0xaaa"))

	ur := newRead("0xaaa", "0xTX", 100, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING)
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	require.True(t, f.k.HasUniversalRead(f.ctx, "0xaaa"))
	got, found := f.k.GetUniversalRead(f.ctx, "0xaaa")
	require.True(t, found)
	require.Equal(t, "0xaaa", got.Id)
	require.Equal(t, "eip155:1", got.Request.DestinationChain)

	_, found = f.k.GetUniversalRead(f.ctx, "0xmissing")
	require.False(t, found)
}

func TestSetUniversalRead_RejectsEmptyID(t *testing.T) {
	f := SetupTest(t)
	require.Error(t, f.k.SetUniversalRead(f.ctx, types.UniversalRead{}))
}
