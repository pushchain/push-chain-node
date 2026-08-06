package keeper_test

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	evmtypes "github.com/cosmos/evm/x/vm/types"

	"github.com/pushchain/push-chain-node/x/ucallback/types"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

type acct struct {
	ChainNamespace string
	ChainId        string
	Owner          []byte
}

type spec struct {
	Account               acct
	Query                 []byte
	MinConfirmations      uint16
	BlockNumber           uint64
	ExpiryPushChainHeight uint64
	MaxFee                *big.Int
}

func readSpecArgs(t *testing.T) abi.Arguments {
	t.Helper()
	specType, err := abi.NewType("tuple", "", []abi.ArgumentMarshaling{
		{Name: "account", Type: "tuple", Components: []abi.ArgumentMarshaling{
			{Name: "chainNamespace", Type: "string"},
			{Name: "chainId", Type: "string"},
			{Name: "owner", Type: "bytes"},
		}},
		{Name: "query", Type: "bytes"},
		{Name: "minConfirmations", Type: "uint16"},
		{Name: "blockNumber", Type: "uint64"},
		{Name: "expiryPushChainHeight", Type: "uint64"},
		{Name: "maxFee", Type: "uint256"},
	})
	require.NoError(t, err)
	u256, err := abi.NewType("uint256", "", nil)
	require.NoError(t, err)
	return abi.Arguments{{Type: specType}, {Type: u256}}
}

// readLog builds a well-formed ReadRequested log emitted by the real system
// contract address.
func readLog(t *testing.T, requestID string, expiry uint64, index uint64) *evmtypes.Log {
	t.Helper()
	data, err := readSpecArgs(t).Pack(spec{
		Account: acct{
			ChainNamespace: "eip155",
			ChainId:        "11155111",
			Owner:          common.FromHex("0x1111111111111111111111111111111111111111"),
		},
		Query:                 common.FromHex("0xdeadbeef"),
		MinConfirmations:      6,
		BlockNumber:           8_000_000,
		ExpiryPushChainHeight: expiry,
		MaxFee:                big.NewInt(7),
	}, big.NewInt(99))
	require.NoError(t, err)

	return &evmtypes.Log{
		Address: uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address,
		Topics: []string{
			types.ReadRequestedEventSig.Hex(),
			common.HexToHash(requestID).Hex(),
			common.HexToHash("0x2222222222222222222222222222222222222222").Hex(),
			common.HexToHash("0x3333333333333333333333333333333333333333").Hex(),
		},
		Data:  data,
		Index: index,
	}
}

func receipt(hash string, logs ...*evmtypes.Log) *evmtypes.MsgEthereumTxResponse {
	return &evmtypes.MsgEthereumTxResponse{Hash: hash, Logs: logs}
}

func TestIngestReadRequests_RecordsPendingRead(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(4242)

	lg := readLog(t, "0xaa", 900_000, 3)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))

	ur, found := f.k.GetUniversalRead(f.ctx, lg.Topics[1])
	require.True(t, found)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status)

	r := ur.Request
	require.Equal(t, "eip155:11155111", r.DestinationChain, "namespace and id joined to CAIP-2")
	require.Equal(t, common.FromHex("0x1111111111111111111111111111111111111111"), r.Owner)
	require.Equal(t, common.FromHex("0xdeadbeef"), r.Query)
	require.Equal(t, uint32(6), r.MinConfirmations)
	require.Equal(t, uint64(8_000_000), r.DestinationBlockHeight)
	require.Equal(t, uint64(900_000), r.ExpiryBlockHeight)
	require.Equal(t, uint64(4242), r.CreatedAtHeight, "taken from the block, not the event")
	require.Equal(t, "0xTX", r.RequestedTxHash)
	require.Equal(t, uint64(3), r.RequestedLogIndex)
	require.Equal(t, "99", r.FeesDeposited)
	require.Equal(t, "7", r.MaxFee)
}

// The address filter is the whole trust boundary: topic0 alone is forgeable by
// any contract, so a matching event from elsewhere must be ignored entirely.
func TestIngestReadRequests_IgnoresForeignContract(t *testing.T) {
	f := SetupTest(t)

	lg := readLog(t, "0xaa", 900_000, 0)
	lg.Address = "0x000000000000000000000000000000000000dEaD"

	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))
	require.False(t, f.k.HasUniversalRead(f.ctx, lg.Topics[1]),
		"a ReadRequested-shaped log from a foreign address must not mint a request")
}

func TestIngestReadRequests_IgnoresUnrelatedLogs(t *testing.T) {
	f := SetupTest(t)

	other := &evmtypes.Log{
		Address: uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address,
		Topics:  []string{common.HexToHash("0xfeed").Hex()},
		Data:    []byte{1, 2, 3},
	}
	noTopics := &evmtypes.Log{
		Address: uregistrytypes.SYSTEM_CONTRACTS["UNIVERSAL_CALLBACK"].Address,
	}

	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", other, noTopics)))
	require.Empty(t, pendingIDs(t, f))
}

func TestIngestReadRequests_SkipsRemovedLogs(t *testing.T) {
	f := SetupTest(t)

	lg := readLog(t, "0xaa", 900_000, 0)
	lg.Removed = true

	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))
	require.False(t, f.k.HasUniversalRead(f.ctx, lg.Topics[1]))
}

// One transaction emitting several ReadRequested logs becomes several independent
// records that still reassemble as a batch.
func TestIngestReadRequests_Batch(t *testing.T) {
	f := SetupTest(t)

	a := readLog(t, "0xaa", 900_000, 0)
	b := readLog(t, "0xbb", 900_001, 1)
	c := readLog(t, "0xcc", 900_002, 2)

	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xBATCH", a, b, c)))

	res, err := f.queryServer.ReadsByTx(f.ctx, &types.QueryReadsByTxRequest{TxHash: "0xBATCH"})
	require.NoError(t, err)
	require.Len(t, res.Reads, 3)

	for _, r := range res.Reads {
		require.Equal(t, "0xBATCH", r.Request.RequestedTxHash)
		require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, r.Status)
	}
	// log index is preserved per sibling, so each is individually addressable
	require.ElementsMatch(t, []uint64{0, 1, 2},
		[]uint64{res.Reads[0].Request.RequestedLogIndex,
			res.Reads[1].Request.RequestedLogIndex,
			res.Reads[2].Request.RequestedLogIndex})
}

// Replaying the same log must not overwrite progress already made on the request.
func TestIngestReadRequests_IsIdempotent(t *testing.T) {
	f := SetupTest(t)

	lg := readLog(t, "0xaa", 900_000, 0)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))

	// the request advances
	ur, found := f.k.GetUniversalRead(f.ctx, lg.Topics[1])
	require.True(t, found)
	ur.Status = types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING
	ur.BallotKey = "ballot-1"
	require.NoError(t, f.k.SetUniversalRead(f.ctx, ur))

	// replay
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))

	again, found := f.k.GetUniversalRead(f.ctx, lg.Topics[1])
	require.True(t, found)
	require.Equal(t, types.UniversalReadStatus_UNIVERSAL_READ_STATUS_VOTING, again.Status,
		"replay must not reset an in-flight request to PENDING")
	require.Equal(t, "ballot-1", again.BallotKey)
}

// An undecodable log from our own contract is a bug, not user error. Returning an
// error reverts the EVM tx so the funder keeps their fee rather than paying for a
// request no validator will ever see.
func TestIngestReadRequests_UndecodableIsAnError(t *testing.T) {
	f := SetupTest(t)

	lg := readLog(t, "0xaa", 900_000, 0)
	lg.Data = lg.Data[:len(lg.Data)/2]

	require.Error(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", lg)))
	require.Empty(t, pendingIDs(t, f))
}

func TestIngestReadRequests_EmptyReceipt(t *testing.T) {
	f := SetupTest(t)
	require.NoError(t, f.k.IngestReadRequests(f.ctx, nil))
	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX")))
	require.Empty(t, pendingIDs(t, f))
}

// Ingested reads are immediately visible to validators through the polling query.
func TestIngestReadRequests_VisibleToValidators(t *testing.T) {
	f := SetupTest(t)
	f.ctx = f.ctx.WithBlockHeight(100)

	live := readLog(t, "0xaa", 500, 0)
	dead := readLog(t, "0xbb", 50, 1)

	require.NoError(t, f.k.IngestReadRequests(f.ctx, receipt("0xTX", live, dead)))

	require.Equal(t, []string{live.Topics[1]}, pendingIDs(t, f),
		"a request ingested already past its expiry is recorded but never offered")
	require.True(t, f.k.HasUniversalRead(f.ctx, dead.Topics[1]))
}
