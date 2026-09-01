package evm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// helper to create an in-memory DB for tests
func testDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	return database
}

func testLogger(t *testing.T) zerolog.Logger {
	t.Helper()
	return zerolog.New(zerolog.NewTestWriter(t))
}
func TestNewEventListener_EmptyGatewayAddress(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "", "", "eip155:1", nil, nil, database, 5, nil, logger)
	assert.Nil(t, el)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "gateway address not configured")
}

func TestNewEventListener_EmptyChainID(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "", "", nil, nil, database, 5, nil, logger)
	assert.Nil(t, el)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chain ID not configured")
}

func TestNewEventListener_ValidCreation(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 10, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	assert.Equal(t, "0xGateway", el.gatewayAddress)
	assert.Equal(t, "0xVault", el.vaultAddress)
	assert.Equal(t, "eip155:1", el.chainID)
	assert.Equal(t, 10, el.eventPollingSeconds)
	assert.NotNil(t, el.database)
	assert.NotNil(t, el.chainStore)
	assert.NotNil(t, el.stopCh)
	assert.False(t, el.running)
}

func TestNewEventListener_NilDatabaseAllowed(t *testing.T) {
	// NewEventListener does not validate database being nil; it passes it through
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Nil(t, el.database)
}
func TestEventListener_IsRunning_DefaultFalse(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.False(t, el.IsRunning())
}

func TestEventListener_StartSetsRunning(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	// Use no topics so the listen goroutine exits early (before hitting nil rpcClient)
	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	// The goroutine will exit quickly because there are no event topics configured.
	time.Sleep(50 * time.Millisecond)

	// Stop should work cleanly
	err = el.Stop()
	assert.NoError(t, err)
	assert.False(t, el.IsRunning())
}

func TestEventListener_DoubleStartReturnsError(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)

	err = el.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// cleanup
	time.Sleep(50 * time.Millisecond)
	el.Stop()
}

func TestEventListener_StopWhenNotRunning(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	// Stop on a listener that was never started should be a no-op
	err = el.Stop()
	assert.NoError(t, err)
	assert.False(t, el.IsRunning())
}

func TestEventListener_StartStopStart(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx := context.Background()

	// First start (no topics, so goroutine exits immediately)
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	time.Sleep(50 * time.Millisecond)
	err = el.Stop()
	require.NoError(t, err)
	assert.False(t, el.IsRunning())

	// Second start after stop should work
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	time.Sleep(50 * time.Millisecond)
	el.Stop()
}
func TestNewEventListener_TopicMapFromGatewayMethods(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	sendFundsTopicHex := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	revertTxTopicHex := "0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: sendFundsTopicHex},
		{Name: EventTypeRevertUniversalTx, Identifier: "revertUniversalTx()", EventIdentifier: revertTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 2)
	assert.Len(t, el.topicToEventType, 2)

	assert.Equal(t, EventTypeSendFunds, el.topicToEventType[ethcommon.HexToHash(sendFundsTopicHex)])
	assert.Equal(t, EventTypeRevertUniversalTx, el.topicToEventType[ethcommon.HexToHash(revertTxTopicHex)])
}

func TestNewEventListener_TopicMapFromVaultMethods(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	finalizeTxTopicHex := "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: EventTypeFinalizeUniversalTx, Identifier: "finalizeUniversalTx()", EventIdentifier: finalizeTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 1)
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.topicToEventType[ethcommon.HexToHash(finalizeTxTopicHex)])
}

func TestNewEventListener_TopicMapCombined(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	sendFundsTopicHex := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	finalizeTxTopicHex := "0xdddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: sendFundsTopicHex},
	}
	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: EventTypeFinalizeUniversalTx, Identifier: "finalizeUniversalTx()", EventIdentifier: finalizeTxTopicHex},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 2)
	assert.Len(t, el.topicToEventType, 2)

	assert.Equal(t, EventTypeSendFunds, el.topicToEventType[ethcommon.HexToHash(sendFundsTopicHex)])
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.topicToEventType[ethcommon.HexToHash(finalizeTxTopicHex)])
}

func TestNewEventListener_EmptyEventIdentifierSkipped(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, Identifier: "sendFunds()", EventIdentifier: ""}, // empty => skip
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}

func TestNewEventListener_UnknownMethodNameSkipped(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	gatewayMethods := []*uregistrytypes.GatewayMethods{
		{Name: "unknownMethod", Identifier: "unknown()", EventIdentifier: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"},
	}
	vaultMethods := []*uregistrytypes.VaultMethods{
		{Name: "unknownVaultMethod", Identifier: "unknown()", EventIdentifier: "0xffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"},
	}

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", gatewayMethods, vaultMethods, database, 5, nil, logger)
	require.NoError(t, err)

	// Unknown names should not be added to topic map
	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}

func TestNewEventListener_NoMethodsProducesEmptyTopics(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)

	assert.Len(t, el.eventTopics, 0)
	assert.Len(t, el.topicToEventType, 0)
}
func TestEventListener_GetPollingInterval(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	t.Run("Custom interval", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 15, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, el.getPollingInterval())
	})

	t.Run("Zero defaults to 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 0, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("Negative defaults to 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, -1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})
}
func TestNewEventListener_EventStartFromStored(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	startBlock := int64(12345)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)
	require.NotNil(t, el.eventStartFrom)
	assert.Equal(t, int64(12345), *el.eventStartFrom)
}

func TestNewEventListener_EventStartFromNil(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
	require.NoError(t, err)
	assert.Nil(t, el.eventStartFrom)
}
func TestEventListener_GetStartBlockFromConfig(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	t.Run("positive eventStartFrom returns that block", func(t *testing.T) {
		startBlock := int64(5000)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(5000), block)
	})

	t.Run("zero eventStartFrom returns 0", func(t *testing.T) {
		startBlock := int64(0)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(0), block)
	})

	t.Run("large positive eventStartFrom", func(t *testing.T) {
		startBlock := int64(999999999)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		block, err := el.getStartBlockFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(999999999), block)
	})

	t.Run("minus one eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		startBlock := int64(-1)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		// rpcClient is nil, so calling GetLatestBlock panics
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})

	t.Run("nil eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, nil, logger)
		require.NoError(t, err)

		// nil rpcClient, nil eventStartFrom -> falls through to rpcClient.GetLatestBlock which panics on nil
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})

	t.Run("negative value less than -1 with nil rpcClient panics", func(t *testing.T) {
		startBlock := int64(-5)
		el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
		require.NoError(t, err)

		// -5 is < 0 but not -1, and not >= 0, so falls through to rpcClient.GetLatestBlock
		assert.Panics(t, func() {
			el.getStartBlockFromConfig(context.Background())
		})
	})
}

func TestEventListener_ContextCancellationStopsGoroutine(t *testing.T) {
	database := testDB(t)
	logger := testLogger(t)

	// Use no topics so the goroutine exits at the "no event topics" warning
	// before trying to use nil rpcClient. We can still verify context flow.
	startBlock := int64(100)
	el, err := NewEventListener(nil, "0xGateway", "0xVault", "eip155:1", nil, nil, database, 5, &startBlock, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	// Cancel context
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Stop to clean up
	el.Stop()
	assert.False(t, el.IsRunning())
}

// logQueryServer serves eth_getLogs, rejecting any query whose block span exceeds
// maxSpan the way a provider rejects an over-large result set, and recording the
// spans it was asked for so tests can assert how the client adapted.
type logQueryServer struct {
	maxSpan  uint64
	failFrom uint64 // when non-zero, reject any query overlapping this block onwards
	mu       sync.Mutex
	asked    [][2]uint64
	served   [][2]uint64 // only the queries that actually returned logs
}

func (s *logQueryServer) record(from, to uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.asked = append(s.asked, [2]uint64{from, to})
}

func (s *logQueryServer) spans() [][2]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]uint64(nil), s.asked...)
}

func (s *logQueryServer) servedSpans() [][2]uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]uint64(nil), s.served...)
}

func (s *logQueryServer) recordServed(from, to uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.served = append(s.served, [2]uint64{from, to})
}

func (s *logQueryServer) start(t *testing.T) *RPCClient {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")

		if !strings.Contains(string(body), "eth_getLogs") {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1"}`))
			return
		}

		var req struct {
			Params []struct {
				FromBlock string `json:"fromBlock"`
				ToBlock   string `json:"toBlock"`
			} `json:"params"`
		}
		_ = json.Unmarshal(body, &req)
		from, _ := strconv.ParseUint(strings.TrimPrefix(req.Params[0].FromBlock, "0x"), 16, 64)
		to, _ := strconv.ParseUint(strings.TrimPrefix(req.Params[0].ToBlock, "0x"), 16, 64)
		s.record(from, to)

		overSpan := to-from+1 > s.maxSpan
		stuck := s.failFrom != 0 && to >= s.failFrom
		if overSpan || stuck {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32005,"message":"query returned more than 10000 results"}}`))
			return
		}
		s.recordServed(from, to)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":[]}`))
	}))
	t.Cleanup(srv.Close)

	rpcClient, err := NewRPCClient([]string{srv.URL}, 1, zerolog.Nop())
	require.NoError(t, err)
	t.Cleanup(rpcClient.Close)
	return rpcClient
}

func newRangeListener(t *testing.T, rpcClient *RPCClient) *EventListener {
	t.Helper()
	el, err := NewEventListener(rpcClient, "0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222", "eip155:1", nil, nil, testDB(t), 10, nil, zerolog.Nop())
	require.NoError(t, err)
	return el
}

// Providers cap the result set, not the block count, so a dense window is
// rejected at a span that is normally fine. A fixed span retries the same
// rejected query forever and the cursor never moves past it.
func TestProcessBlockRange_ShrinksSpanUntilTheQueryFits(t *testing.T) {
	srv := &logQueryServer{maxSpan: 1000} // anything wider than 1000 blocks is rejected
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 1, 2000, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(2001), next, "the whole range must end up covered")

	spans := srv.spans()
	require.NotEmpty(t, spans)

	// The first attempt is optimistic, and every retry restarts at the same block
	// rather than skipping the blocks that were rejected.
	assert.Equal(t, uint64(1), spans[0][0])
	assert.Equal(t, uint64(2000), spans[0][1], "first attempt spans the whole range")

	var widths []uint64
	for _, s := range spans {
		if s[0] == 1 {
			widths = append(widths, s[1]-s[0]+1)
		}
	}
	require.Greater(t, len(widths), 1, "must retry the same start over a smaller span")
	for i := 1; i < len(widths); i++ {
		assert.Less(t, widths[i], widths[i-1], "each retry must be narrower")
	}
}

// A window that cannot be read even at the floor must not be stepped over:
// the blocks may contain deposits, and skipping them loses those permanently.
func TestProcessBlockRange_DoesNotSkipAnUnreadableWindow(t *testing.T) {
	srv := &logQueryServer{maxSpan: maxBlockRange, failFrom: 1} // every query fails
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 1, 500, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "minimum span")
	assert.Equal(t, uint64(1), next, "cursor must stay put, not advance past unread blocks")
}

// Work already done must be committed. Holding the cursor at the start would
// re-read the earlier chunks on every tick, so one bad window would sit in front
// of everything behind it.
func TestProcessBlockRange_ReportsPartialProgressOnFailure(t *testing.T) {
	// First 9000 blocks are readable; anything from 9001 always fails.
	srv := &logQueryServer{maxSpan: maxBlockRange, failFrom: 9001}
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 1, 20000, nil)
	require.Error(t, err)
	assert.Equal(t, uint64(9001), next, "must report the first block it could not cover")
}

func TestProcessBlockRange_SinglePassWhenNothingIsRejected(t *testing.T) {
	srv := &logQueryServer{maxSpan: maxBlockRange}
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 1, 500, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(501), next)
	assert.Len(t, srv.spans(), 1, "a range that fits must not be split")
}

// assertExactCoverage checks that the served queries tile [from,to] with no gap
// and no block fetched twice. A gap is a block whose logs are never read, which
// for an inbound is a deposit nobody observes.
func assertExactCoverage(t *testing.T, served [][2]uint64, from, to uint64) {
	t.Helper()

	sort.Slice(served, func(i, j int) bool { return served[i][0] < served[j][0] })

	require.NotEmpty(t, served, "nothing was fetched for %d-%d", from, to)
	assert.Equal(t, from, served[0][0], "coverage must start at the first block")
	assert.Equal(t, to, served[len(served)-1][1], "coverage must end at the last block")

	for i := 1; i < len(served); i++ {
		prevEnd, thisStart := served[i-1][1], served[i][0]
		assert.Equal(t, prevEnd+1, thisStart,
			"chunk %d starts at %d but the previous ended at %d", i, thisStart, prevEnd)
	}

	var covered uint64
	for _, c := range served {
		require.LessOrEqual(t, c[0], c[1], "chunk %d-%d is inverted", c[0], c[1])
		covered += c[1] - c[0] + 1
	}
	assert.Equal(t, to-from+1, covered, "total blocks covered must equal the range size")
}

// Every block in the range must be fetched exactly once, whatever the span ends
// up being. Off-by-one at a chunk boundary would silently skip a block.
func TestProcessBlockRange_CoversEveryBlockExactlyOnce(t *testing.T) {
	cases := []struct {
		name       string
		from, to   uint64
		serverSpan uint64 // widest query the server will accept
	}{
		{"single block", 1, 1, maxBlockRange},
		{"single block at zero", 0, 0, maxBlockRange},
		{"range starting at zero", 0, 500, maxBlockRange},
		{"exactly one full span", 1, maxBlockRange, maxBlockRange},
		{"one block past a full span", 1, maxBlockRange + 1, maxBlockRange},
		{"one block short of a full span", 1, maxBlockRange - 1, maxBlockRange},
		{"several full spans", 1, maxBlockRange * 3, maxBlockRange},
		{"several spans plus a remainder", 1, maxBlockRange*2 + 137, maxBlockRange},
		{"forced shrink, divisible", 1, 2000, 1000},
		{"forced shrink, not divisible", 1, 2500, 333},
		{"forced shrink to the floor", 1, 1000, minBlockRange},
		{"shrink with an odd start", 4097, 9999, 700},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := &logQueryServer{maxSpan: tc.serverSpan}
			el := newRangeListener(t, srv.start(t))

			next, err := el.processBlockRange(context.Background(), tc.from, tc.to, nil)
			require.NoError(t, err)
			assert.Equal(t, tc.to+1, next, "must report the range as fully covered")

			assertExactCoverage(t, srv.servedSpans(), tc.from, tc.to)
		})
	}
}

// After a shrink the walk continues at the smaller span. The blocks either side
// of the failure boundary must still be covered exactly once.
func TestProcessBlockRange_NoGapAroundAShrink(t *testing.T) {
	srv := &logQueryServer{maxSpan: 750}
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 100, 3100, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(3101), next)

	assertExactCoverage(t, srv.servedSpans(), 100, 3100)
}

// Across successive polls the caller resumes from the block the previous call
// reported, so a partial range must hand back a boundary that leaves no hole.
func TestProcessBlockRange_ResumeAfterPartialLeavesNoGap(t *testing.T) {
	// Blocks from 5001 are unreadable, so the first call stops there.
	srv := &logQueryServer{maxSpan: maxBlockRange, failFrom: 5001}
	el := newRangeListener(t, srv.start(t))

	next, err := el.processBlockRange(context.Background(), 1, 8000, nil)
	require.Error(t, err)
	assertExactCoverage(t, srv.servedSpans(), 1, next-1)

	// The obstruction clears and the caller resumes from where it stopped.
	srv2 := &logQueryServer{maxSpan: maxBlockRange}
	el2 := newRangeListener(t, srv2.start(t))

	final, err := el2.processBlockRange(context.Background(), next, 8000, nil)
	require.NoError(t, err)
	assert.Equal(t, uint64(8001), final)
	assertExactCoverage(t, srv2.servedSpans(), next, 8000)
}
