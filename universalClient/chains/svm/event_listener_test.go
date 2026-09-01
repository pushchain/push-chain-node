package svm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/mr-tron/base58"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/store"
	uregistrytypes "github.com/pushchain/push-chain-node/x/uregistry/types"
)

// mockRPCClient implements rpcClientInterface for tests. Pages are returned
// from `signaturePages` in order; the cursor passed to each call is recorded
// in `sigCallCursors` for assertion. GetTransaction returns (nil, nil) so the
// in-range branch increments `processed` but does no event parsing — keeps
// tests focused on pagination/cursor behavior.
type mockRPCClient struct {
	latestSlot     uint64
	signaturePages [][]*solanarpc.TransactionSignature
	sigCallCursors []solana.Signature
	txCalls        []solana.Signature
}

func (m *mockRPCClient) GetLatestSlot(ctx context.Context) (uint64, error) {
	return m.latestSlot, nil
}

func (m *mockRPCClient) GetSignaturesForAddress(ctx context.Context, address solana.PublicKey, before solana.Signature) ([]*solanarpc.TransactionSignature, error) {
	idx := len(m.sigCallCursors)
	m.sigCallCursors = append(m.sigCallCursors, before)
	if idx >= len(m.signaturePages) {
		return nil, nil
	}
	return m.signaturePages[idx], nil
}

func (m *mockRPCClient) GetTransaction(ctx context.Context, signature solana.Signature) (*solanarpc.GetTransactionResult, error) {
	m.txCalls = append(m.txCalls, signature)
	return nil, nil
}

// mkSig builds a deterministic non-zero solana.Signature from a single byte seed.
// Seed 0 is reserved (would collide with the zero-value cursor).
func mkSig(seed byte) solana.Signature {
	var s solana.Signature
	for i := range s {
		s[i] = seed
	}
	return s
}

func mkSigInfo(slot uint64, seed byte) *solanarpc.TransactionSignature {
	return &solanarpc.TransactionSignature{
		Slot:      slot,
		Signature: mkSig(seed),
	}
}

func TestNewEventListener_Valid(t *testing.T) {
	logger := zerolog.Nop()
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "abcdef0123456789",
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr111111111111111111111111111111111", "solana:test", methods, database, 10, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	assert.Equal(t, "solana:test", el.chainID)
	assert.Equal(t, "GatewayAddr111111111111111111111111111111111", el.gatewayAddress)
	assert.Equal(t, 10, el.eventPollingSeconds)
	assert.False(t, el.running)
	assert.NotNil(t, el.stopCh)
}

func TestNewEventListener_EmptyGateway(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "", "solana:test", nil, nil, 5, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, el)
	assert.Contains(t, err.Error(), "gateway address not configured")
}

func TestNewEventListener_EmptyChainID(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "", nil, nil, 5, nil, logger)
	assert.Error(t, err)
	assert.Nil(t, el)
	assert.Contains(t, err.Error(), "chain ID not configured")
}

func TestNewEventListener_NilRPCClient(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Nil(t, el.rpcClient)
}

func TestNewEventListener_NilMethods(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	assert.Empty(t, el.discriminatorToEventType)
}

func TestNewEventListener_DiscriminatorMapping(t *testing.T) {
	logger := zerolog.Nop()

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "AABB0011CCDD2233",
		},
		{
			Name:            EventTypeFinalizeUniversalTx,
			EventIdentifier: "1122334455667788",
		},
		{
			Name:            EventTypeRevertUniversalTx,
			EventIdentifier: "DEADBEEF01234567",
		},
		{
			Name:            "unknown_method", // not a recognized event type
			EventIdentifier: "ffffffffffffffff",
		},
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: "", // empty identifier should be skipped
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", methods, nil, 5, nil, logger)
	require.NoError(t, err)
	require.NotNil(t, el)

	// Should have 3 entries (unknown_method skipped, empty identifier skipped)
	assert.Len(t, el.discriminatorToEventType, 3)
	assert.Equal(t, EventTypeSendFunds, el.discriminatorToEventType["aabb0011ccdd2233"])
	assert.Equal(t, EventTypeFinalizeUniversalTx, el.discriminatorToEventType["1122334455667788"])
	assert.Equal(t, EventTypeRevertUniversalTx, el.discriminatorToEventType["deadbeef01234567"])
}

func TestNewEventListener_EventStartFrom(t *testing.T) {
	logger := zerolog.Nop()

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, &startFrom, logger)
	require.NoError(t, err)
	require.NotNil(t, el)
	require.NotNil(t, el.eventStartFrom)
	assert.Equal(t, int64(100), *el.eventStartFrom)
}

func TestEventListener_GetPollingInterval(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("positive value returns configured interval", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 15, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 15*time.Second, el.getPollingInterval())
	})

	t.Run("zero returns default 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 0, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("negative returns default 5s", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, -1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, el.getPollingInterval())
	})

	t.Run("one second", func(t *testing.T) {
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 1, nil, logger)
		require.NoError(t, err)
		assert.Equal(t, 1*time.Second, el.getPollingInterval())
	})
}

func TestEventListener_DetermineEventType(t *testing.T) {
	logger := zerolog.Nop()

	// Build a known discriminator: 8 bytes -> hex -> lowercase
	discriminatorBytes := []byte{0xAA, 0xBB, 0xCC, 0xDD, 0x11, 0x22, 0x33, 0x44}
	discriminatorHex := hex.EncodeToString(discriminatorBytes) // "aabbccdd11223344"

	methods := []*uregistrytypes.GatewayMethods{
		{
			Name:            EventTypeSendFunds,
			EventIdentifier: discriminatorHex,
		},
	}

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", methods, nil, 5, nil, logger)
	require.NoError(t, err)

	t.Run("matching discriminator returns event type", func(t *testing.T) {
		payload := append(discriminatorBytes, []byte("extra data here")...)
		encoded := base64.StdEncoding.EncodeToString(payload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Equal(t, EventTypeSendFunds, eventType)
	})

	t.Run("non-matching discriminator returns empty", func(t *testing.T) {
		otherBytes := []byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF}
		payload := append(otherBytes, []byte("extra")...)
		encoded := base64.StdEncoding.EncodeToString(payload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("no Program data prefix returns empty", func(t *testing.T) {
		eventType := el.determineEventType("Some other log message")
		assert.Empty(t, eventType)
	})

	t.Run("invalid base64 returns empty", func(t *testing.T) {
		eventType := el.determineEventType("Program data: !!!invalid-base64!!!")
		assert.Empty(t, eventType)
	})

	t.Run("payload shorter than 8 bytes returns empty", func(t *testing.T) {
		shortPayload := []byte{0xAA, 0xBB, 0xCC}
		encoded := base64.StdEncoding.EncodeToString(shortPayload)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("empty log returns empty", func(t *testing.T) {
		eventType := el.determineEventType("")
		assert.Empty(t, eventType)
	})

	t.Run("Program data with empty payload returns empty", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte{})
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Empty(t, eventType)
	})

	t.Run("exactly 8 bytes matching discriminator", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString(discriminatorBytes)
		log := "Program data: " + encoded

		eventType := el.determineEventType(log)
		assert.Equal(t, EventTypeSendFunds, eventType)
	})
}

func TestEventListener_GetStartSlotFromConfig(t *testing.T) {
	logger := zerolog.Nop()

	t.Run("positive eventStartFrom returns that slot", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(5000)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(5000), slot)
	})

	t.Run("zero eventStartFrom returns 0", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(0)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(0), slot)
	})

	t.Run("large positive eventStartFrom", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(999999999)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		slot, err := el.getStartSlotFromConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, uint64(999999999), slot)
	})

	t.Run("minus one eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(-1)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		// rpcClient is nil, so calling GetLatestSlot panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})

	t.Run("nil eventStartFrom with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, nil, logger)
		require.NoError(t, err)

		// nil rpcClient, nil eventStartFrom -> falls through to rpcClient.GetLatestSlot which panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})

	t.Run("negative value less than -1 with nil rpcClient panics", func(t *testing.T) {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)

		startFrom := int64(-5)
		el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 5, &startFrom, logger)
		require.NoError(t, err)

		// -5 is < 0 but not -1, falls through to rpcClient.GetLatestSlot which panics
		assert.Panics(t, func() {
			el.getStartSlotFromConfig(context.Background())
		})
	})
}

func TestEventListener_ProcessSignatureBatch_NoRPCCalls(t *testing.T) {
	logger := zerolog.Nop()

	// Constructed with nil rpcClient — these scenarios must early-return (via the
	// bounds-check `continue`s) before any RPC call would be made.
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	t.Run("empty batch returns 0", func(t *testing.T) {
		processed, err := el.processSignatureBatch(context.Background(), nil, 100, 200)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), processed)
	})

	t.Run("all sigs below fromSlot return 0", func(t *testing.T) {
		batch := []*solanarpc.TransactionSignature{
			{Slot: 50}, {Slot: 75}, {Slot: 99},
		}
		processed, err := el.processSignatureBatch(context.Background(), batch, 100, 200)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), processed)
	})

	t.Run("all sigs above toSlot return 0 without break", func(t *testing.T) {
		// Regression guard: an upper-bound `break` here would short-circuit;
		// `continue` must skip past sigs > toSlot without aborting. All-above
		// sigs all skip via continue; processed must be 0.
		batch := []*solanarpc.TransactionSignature{
			{Slot: 250}, {Slot: 300}, {Slot: 999},
		}
		processed, err := el.processSignatureBatch(context.Background(), batch, 100, 200)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), processed)
	})

	t.Run("mixed out-of-range sigs (above and below) return 0", func(t *testing.T) {
		// Mixed unordered batch with no in-range entries — exercises both
		// continue branches without invoking the RPC.
		batch := []*solanarpc.TransactionSignature{
			{Slot: 250}, {Slot: 50}, {Slot: 999}, {Slot: 75},
		}
		processed, err := el.processSignatureBatch(context.Background(), batch, 100, 200)
		require.NoError(t, err)
		assert.Equal(t, uint64(0), processed)
	})
}

func TestEventListener_ProcessSlotRange(t *testing.T) {
	logger := zerolog.Nop()
	gateway := solana.SystemProgramID.String() // any valid base58 pubkey

	setup := func(t *testing.T, mock *mockRPCClient) *EventListener {
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)
		el, err := NewEventListener(mock, gateway, "solana:test", nil, database, 5, nil, logger)
		require.NoError(t, err)
		return el
	}

	t.Run("single page, minSlot below fromSlot terminates loop", func(t *testing.T) {
		// Page slots [100, 90, 80, 70, 60]. Window [85, 200]. min=60 < fromSlot=85 → break.
		// In-range = slots 100, 90 → 2 GetTransaction calls.
		mock := &mockRPCClient{
			signaturePages: [][]*solanarpc.TransactionSignature{
				{
					mkSigInfo(100, 1),
					mkSigInfo(90, 2),
					mkSigInfo(80, 3),
					mkSigInfo(70, 4),
					mkSigInfo(60, 5),
				},
			},
		}
		el := setup(t, mock)

		err := el.processSlotRange(context.Background(), 85, 200)
		require.NoError(t, err)

		require.Len(t, mock.sigCallCursors, 1)
		assert.True(t, mock.sigCallCursors[0].IsZero(), "first call should use zero cursor")
		require.Len(t, mock.txCalls, 2)
		assert.Equal(t, mkSig(1), mock.txCalls[0])
		assert.Equal(t, mkSig(2), mock.txCalls[1])
	})

	t.Run("multi-page, cursor advances to min-slot sig", func(t *testing.T) {
		// Page 0: [200, 150]. Page 1: [100, 50]. Window [70, 300].
		// After page 0 minSlot=150 ≥ 70 → continue with cursor = mkSig(2) (slot 150).
		// After page 1 minSlot=50 < 70 → break.
		mock := &mockRPCClient{
			signaturePages: [][]*solanarpc.TransactionSignature{
				{mkSigInfo(200, 1), mkSigInfo(150, 2)},
				{mkSigInfo(100, 3), mkSigInfo(50, 4)},
			},
		}
		el := setup(t, mock)

		err := el.processSlotRange(context.Background(), 70, 300)
		require.NoError(t, err)

		require.Len(t, mock.sigCallCursors, 2)
		assert.True(t, mock.sigCallCursors[0].IsZero())
		assert.Equal(t, mkSig(2), mock.sigCallCursors[1], "page-1 cursor must be page-0 min-slot sig")
		// In-range = 200, 150, 100 (50 below window)
		require.Len(t, mock.txCalls, 3)
	})

	t.Run("empty page terminates immediately", func(t *testing.T) {
		mock := &mockRPCClient{
			signaturePages: [][]*solanarpc.TransactionSignature{{}},
		}
		el := setup(t, mock)

		err := el.processSlotRange(context.Background(), 0, 1000)
		require.NoError(t, err)

		assert.Len(t, mock.sigCallCursors, 1)
		assert.Empty(t, mock.txCalls)
	})

	t.Run("high-slot leading sig does not abort iteration", func(t *testing.T) {
		// Order [{300}, {150}, {100}] — first sig is above toSlot=200. With buggy
		// `break` on upper bound, sigs at 150 and 100 would be missed. With `continue`,
		// both are processed. fromSlot=50 keeps both in range; minSlot=100 > 50 → loop
		// fetches a second (empty) page and terminates there.
		mock := &mockRPCClient{
			signaturePages: [][]*solanarpc.TransactionSignature{
				{mkSigInfo(300, 1), mkSigInfo(150, 2), mkSigInfo(100, 3)},
			},
		}
		el := setup(t, mock)

		err := el.processSlotRange(context.Background(), 50, 200)
		require.NoError(t, err)

		require.Len(t, mock.txCalls, 2, "in-range sigs after the leading out-of-range one must still be processed")
		assert.Equal(t, mkSig(2), mock.txCalls[0])
		assert.Equal(t, mkSig(3), mock.txCalls[1])
	})

	t.Run("cursor uses min-slot sig regardless of array position (https://github.com/solana-labs/solana/issues/22456)", func(t *testing.T) {
		// Page 0 unordered: [200, 50, 150, 80]. min slot = 50 (mkSig(2)).
		// Window [41, 1000] → page 0 minSlot=50 ≥ 41 → continue with cursor = mkSig(2).
		// Page 1: [40] → minSlot=40 < 41 → break.
		mock := &mockRPCClient{
			signaturePages: [][]*solanarpc.TransactionSignature{
				{mkSigInfo(200, 1), mkSigInfo(50, 2), mkSigInfo(150, 3), mkSigInfo(80, 4)},
				{mkSigInfo(40, 5)},
			},
		}
		el := setup(t, mock)

		err := el.processSlotRange(context.Background(), 41, 1000)
		require.NoError(t, err)

		require.Len(t, mock.sigCallCursors, 2)
		assert.Equal(t, mkSig(2), mock.sigCallCursors[1], "page-1 cursor must be the min-slot sig from page 0, not batch[len-1]")
		// All 4 page-0 sigs are in-range (200, 50, 150, 80 all > 41); page-1 sig at slot 40 is not
		require.Len(t, mock.txCalls, 4)
	})
}

func TestEventListener_LargePollWarning(t *testing.T) {
	// Build pages that cumulatively cross largePollWarnThreshold (100k). Threshold is
	// reached after 100 pages of 1000; we add 5 more pages so the warning re-fires
	// for each subsequent page while the condition holds.
	const pagesAfterThreshold = 5
	const totalPages = int(largePollWarnThreshold/1000) + pagesAfterThreshold

	pages := make([][]*solanarpc.TransactionSignature, 0, totalPages+1)
	slot := uint64(2_000_000)
	for p := 0; p < totalPages; p++ {
		page := make([]*solanarpc.TransactionSignature, 1000)
		for i := 0; i < 1000; i++ {
			slot--
			// Seed varies per page to give each page a distinct min-slot sig.
			page[i] = &solanarpc.TransactionSignature{Slot: slot, Signature: mkSig(byte((p % 254) + 1))}
		}
		pages = append(pages, page)
	}
	pages = append(pages, []*solanarpc.TransactionSignature{}) // empty page terminates

	mock := &mockRPCClient{signaturePages: pages}
	var logBuf bytes.Buffer
	logger := zerolog.New(&logBuf).Level(zerolog.WarnLevel)
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	el, err := NewEventListener(mock, solana.SystemProgramID.String(), "solana:test", nil, database, 5, nil, logger)
	require.NoError(t, err)

	err = el.processSlotRange(context.Background(), 0, 3_000_000)
	require.NoError(t, err)

	output := logBuf.String()
	warnCount := strings.Count(output, "large signature backlog being processed")
	// Warning fires once on the page that crosses 100k, and again on each subsequent
	// page while still over threshold. With 5 pages added past threshold, expect 6 warns.
	assert.GreaterOrEqual(t, warnCount, pagesAfterThreshold, "warning should re-emit per page while above threshold")
}

func TestEventListener_StopNotRunning(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	err = el.Stop()
	assert.NoError(t, err)
}

func TestEventListener_IsRunning_InitiallyFalse(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	assert.False(t, el.IsRunning())
}

func TestEventListener_StopTwice(t *testing.T) {
	logger := zerolog.Nop()

	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, nil, 5, nil, logger)
	require.NoError(t, err)

	assert.NoError(t, el.Stop())
	assert.NoError(t, el.Stop())
}

func TestEventListener_StartStop_ContextCancel(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	// Use eventStartFrom to avoid rpcClient calls in getStartSlot
	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())

	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	cancel()

	done := make(chan struct{})
	go func() {
		el.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("event listener did not stop after context cancellation")
	}
}

func TestEventListener_StartStop_StopMethod(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx := context.Background()
	err = el.Start(ctx)
	require.NoError(t, err)
	assert.True(t, el.IsRunning())

	done := make(chan struct{})
	go func() {
		stopErr := el.Stop()
		assert.NoError(t, stopErr)
		close(done)
	}()

	select {
	case <-done:
		assert.False(t, el.IsRunning())
	case <-time.After(5 * time.Second):
		t.Fatal("event listener did not stop after Stop() call")
	}
}

func TestEventListener_StartWhileRunning(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)

	startFrom := int64(100)
	el, err := NewEventListener(nil, "GatewayAddr", "solana:test", nil, database, 1, &startFrom, logger)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = el.Start(ctx)
	require.NoError(t, err)

	// Starting again while running should return an error
	err = el.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Clean up
	cancel()
	el.wg.Wait()
}

const (
	testGatewayProgram  = "CFVSincHYbETh2k7w6u1ENEkjbSLtveRCEBupKidw2VS"
	testAttackerProgram = "AttackerProgram1111111111111111111111111111"
)

type forgeryRPC struct {
	slot   uint64
	sig    solana.Signature
	txJSON string
}

func (m *forgeryRPC) GetLatestSlot(context.Context) (uint64, error) { return m.slot, nil }

func (m *forgeryRPC) GetSignaturesForAddress(context.Context, solana.PublicKey, solana.Signature) ([]*solanarpc.TransactionSignature, error) {
	return []*solanarpc.TransactionSignature{{Signature: m.sig, Slot: m.slot}}, nil
}

func (m *forgeryRPC) GetTransaction(context.Context, solana.Signature) (*solanarpc.GetTransactionResult, error) {
	var tx solanarpc.GetTransactionResult
	if err := json.Unmarshal([]byte(m.txJSON), &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// txWithEmittedEvent renders a transaction whose only inner instruction is an
// emit_cpi event from `emitter`, alongside whatever the runtime logged.
func txWithEmittedEvent(t *testing.T, emitter string, payload []byte, logs []string) string {
	t.Helper()
	data := append(append([]byte{}, eventIxTag...), payload...)
	logJSON, err := json.Marshal(logs)
	require.NoError(t, err)
	return fmt.Sprintf(`{
		"slot": 100,
		"transaction": {
			"signatures": ["%s"],
			"message": {
				"header": {"numRequiredSignatures":1,"numReadonlySignedAccounts":0,"numReadonlyUnsignedAccounts":1},
				"accountKeys": ["%s","%s"],
				"recentBlockhash": "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM",
				"instructions": []
			}
		},
		"meta": {
			"err": null,
			"logMessages": %s,
			"innerInstructions": [
				{"index":0,"instructions":[{"programIdIndex":1,"accounts":[],"data":"%s","stackHeight":2}]}
			]
		}
	}`, mkSig(7).String(), testGatewayProgram, emitter, string(logJSON), base58.Encode(data))
}

// End-to-end proof that the listener drops a forged event. The same valid
// send_funds payload is served twice: emitted by an attacker program it must be
// ignored, emitted by the gateway it must be stored. With emit_cpi the emitter
// is the inner instruction's own program id, so a forgery cannot be dressed up
// by nesting log lines.
func TestProcessSignatureBatch_RejectsForgedGatewayEvent(t *testing.T) {
	discriminator := "0000000000000000" // buildSendFundsPayload zeroes the discriminator
	payload := buildSendFundsPayload(
		[32]byte{1}, [20]byte{2}, [32]byte{3}, 1_000_000,
		nil, [32]byte{4}, 0, nil, false,
	)

	run := func(t *testing.T, emitter string) int {
		t.Helper()
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)
		t.Cleanup(func() { database.Close() })

		methods := []*uregistrytypes.GatewayMethods{
			{Name: EventTypeSendFunds, EventIdentifier: discriminator},
		}
		rpc := &forgeryRPC{slot: 100, sig: mkSig(7), txJSON: txWithEmittedEvent(t, emitter, payload, nil)}
		el, err := NewEventListener(rpc, testGatewayProgram, "solana:test", methods, database, 10, nil, zerolog.Nop())
		require.NoError(t, err)

		_, err = el.processSignatureBatch(context.Background(), []*solanarpc.TransactionSignature{
			{Signature: mkSig(7), Slot: 100},
		}, 0, 200)
		require.NoError(t, err)

		events, err := common.NewChainStore(database).GetPendingEvents(100)
		require.NoError(t, err)
		return len(events)
	}

	t.Run("forged by attacker program is not stored", func(t *testing.T) {
		assert.Zero(t, run(t, testAttackerProgram), "forged gateway event must not become an inbound")
	})

	t.Run("same payload from the gateway is stored", func(t *testing.T) {
		assert.Equal(t, 1, run(t, testGatewayProgram), "genuine gateway event must be observed")
	})
}

// A failed transaction rolls every state change back, but its meta still
// carries the logs and inner instructions that ran before it aborted. Voting
// success off one would credit a transfer that never happened.
func TestProcessSignatureBatch_SkipsFailedTransactions(t *testing.T) {
	payload := buildSendFundsPayload(
		[32]byte{1}, [20]byte{2}, [32]byte{3}, 1_000_000,
		nil, [32]byte{4}, 0, nil, false,
	)
	methods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, EventIdentifier: "0000000000000000"},
	}

	run := func(t *testing.T, txJSON string) int {
		t.Helper()
		database, err := db.OpenInMemoryDB(true)
		require.NoError(t, err)
		t.Cleanup(func() { database.Close() })

		rpc := &forgeryRPC{slot: 100, sig: mkSig(7), txJSON: txJSON}
		el, err := NewEventListener(rpc, testGatewayProgram, "solana:test", methods, database, 10, nil, zerolog.Nop())
		require.NoError(t, err)

		_, err = el.processSignatureBatch(context.Background(), []*solanarpc.TransactionSignature{
			{Signature: mkSig(7), Slot: 100},
		}, 0, 200)
		require.NoError(t, err)

		events, err := common.NewChainStore(database).GetPendingEvents(100)
		require.NoError(t, err)
		return len(events)
	}

	ok := txWithEmittedEvent(t, testGatewayProgram, payload, nil)

	t.Run("succeeded transaction is stored", func(t *testing.T) {
		assert.Equal(t, 1, run(t, ok))
	})

	t.Run("failed transaction is skipped", func(t *testing.T) {
		failed := strings.Replace(ok, `"err": null`, `"err": {"InstructionError":[0,{"Custom":6020}]}`, 1)
		require.NotEqual(t, ok, failed, "the fixture must actually carry a failure")
		assert.Zero(t, run(t, failed),
			"an event emitted before the abort must not be observed")
	})
}

// The point of emit_cpi: the event lives in an inner instruction, so a
// destination CPI that floods the log buffer can no longer take the terminal
// event down with it. This is the F-2026-18817 observation half.
func TestProcessSignatureBatch_TruncatedLogsStillYieldEvent(t *testing.T) {
	database, err := db.OpenInMemoryDB(true)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })

	payload := buildSendFundsPayload(
		[32]byte{1}, [20]byte{2}, [32]byte{3}, 1_000_000,
		nil, [32]byte{4}, 0, nil, false,
	)
	truncated := []string{
		"Program " + testGatewayProgram + " invoke [1]",
		"Program log: truncated",
	}

	methods := []*uregistrytypes.GatewayMethods{
		{Name: EventTypeSendFunds, EventIdentifier: "0000000000000000"},
	}
	rpc := &forgeryRPC{slot: 100, sig: mkSig(7), txJSON: txWithEmittedEvent(t, testGatewayProgram, payload, truncated)}
	el, err := NewEventListener(rpc, testGatewayProgram, "solana:test", methods, database, 10, nil, zerolog.Nop())
	require.NoError(t, err)

	_, err = el.processSignatureBatch(context.Background(), []*solanarpc.TransactionSignature{
		{Signature: mkSig(7), Slot: 100},
	}, 0, 200)
	require.NoError(t, err)

	events, err := common.NewChainStore(database).GetPendingEvents(100)
	require.NoError(t, err)
	assert.Len(t, events, 1, "a truncated log buffer must not cost us the event")
}

// devnetFinalizeTx is a real FinalizeUniversalTxWithIxDataRef from the devnet
// gateway, signature 4ye6nTo4oKcEctDza44Zr7QAwHmqhH4qfeBkDjHqE2aFtgxuhdF9dfs1EmBbYiTfwXMvWUupJ592DQQQAGx5Abus.
// It carries no "Program data:" line at all: the gateway emits through
// emit_cpi, so the event is a self-CPI inner instruction instead.
const devnetFinalizeTx = `{"blockTime":1788253699,"meta":{"computeUnitsConsumed":132988,"costUnits":136994,"err":null,"fee":5000,"innerInstructions":[{"index":0,"instructions":[{"accounts":[0,5],"data":"11114pZy3PBZenKB1vn2UptrP91MCBmdVNUDneZKAWH7B8Sg5eCB3E67uKRhm1xbvr4ACz","programIdIndex":10,"stackHeight":2},{"accounts":[0,9],"data":"1111NuBxPLg6vZ28hQWMLnv7auFnJFYGTC4L1MUjrNkN9xikCBvdVStP4KiJr9vvBcWPa","programIdIndex":10,"stackHeight":2},{"accounts":[9,15],"data":"18ukwGkxoTJPx4HkLTz6ZybMUmX1yt47MVERA5H6yQGUdgK","programIdIndex":16,"stackHeight":2},{"accounts":[0,3],"data":"11113ahNe3Yfn6gi8hZhcH9k4YUezY3FJNRHx6tPLUTkr868BMzwWAZDTUtWcrGK2cw1Fx","programIdIndex":10,"stackHeight":2},{"accounts":[0,4,7,9,10,16],"data":"1","programIdIndex":13,"stackHeight":2},{"accounts":[9],"data":"84eT","programIdIndex":16,"stackHeight":3},{"accounts":[0,4],"data":"11113z11NKiYBjwDfL71F9myuy3cwdtTaatpiC6rj9zomYP4qbzHXZVjhEFkM5qi31QeQg","programIdIndex":10,"stackHeight":3},{"accounts":[4],"data":"P","programIdIndex":16,"stackHeight":3},{"accounts":[4,9],"data":"6VKrKvV2EjfdgaesMejJQDnusTVKHbdt2BPiddZq2WzGf","programIdIndex":16,"stackHeight":3},{"accounts":[9,4,9],"data":"6YF7VVXZihvw","programIdIndex":16,"stackHeight":2},{"accounts":[2,0],"data":"3Bxs4R98mv6mmz5M","programIdIndex":10,"stackHeight":2},{"accounts":[2,0],"data":"3Bxs4PckVVt51W8w","programIdIndex":10,"stackHeight":2},{"accounts":[11],"data":"9opCxkAgBxqeR8UTbeow8YC7933mDbBMCEMKiYsmMRqLs1N8zRudTsxXqkeWaXpNdpt2QZhCyZprcPNHfS7EwMkBnfrzuFhd3zMg8ZLA6Uk1BVxrx5nq9s2EYueLPKVCWCvzyKsZqph76dduPdkHBQs7TdFtP5FtvJssMKvwPu5zShUSegxsJLaYKZCjZAcrYscVbEmLzrvehE2s4qYdMGyW58rkamNjPC4BgKoZpPt136NYv31ftYXB4sVrTdjkJogbp9HpWA1BnewRmXuWddeYyGxyiFG3X44mG5ALa7rTuPgcZukdFmahFVfE2Txj","programIdIndex":14,"stackHeight":2}]}],"loadedAddresses":{"readonly":[],"writable":[]},"logMessages":["Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp invoke [1]","Program log: Instruction: FinalizeUniversalTxWithIxDataRef","Program 11111111111111111111111111111111 invoke [2]","Program 11111111111111111111111111111111 success","Program 11111111111111111111111111111111 invoke [2]","Program 11111111111111111111111111111111 success","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [2]","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA consumed 86 of 148619 compute units","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA success","Program 11111111111111111111111111111111 invoke [2]","Program 11111111111111111111111111111111 success","Program ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL invoke [2]","Program log: Create","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [3]","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA consumed 179 of 93967 compute units","Program return: TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA pQAAAAAAAAA=","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA success","Program 11111111111111111111111111111111 invoke [3]","Program 11111111111111111111111111111111 success","Program log: Initialize the associated token account","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [3]","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA consumed 37 of 88878 compute units","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA success","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [3]","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA consumed 233 of 86415 compute units","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA success","Program ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL consumed 15010 of 100888 compute units","Program ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL success","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA invoke [2]","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA consumed 122 of 82575 compute units","Program TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA success","Program 11111111111111111111111111111111 invoke [2]","Program 11111111111111111111111111111111 success","Program 11111111111111111111111111111111 invoke [2]","Program 11111111111111111111111111111111 success","Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp invoke [2]","Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp consumed 2517 of 71342 compute units","Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp success","Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp consumed 132988 of 200000 compute units","Program DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp success"],"postBalances":[5010154600,2793266460,17563563083,1203270,1855569,861288,0,0,1566000,1329930,1,0,2832720,5938070540,1141440,1009200,15367267856],"postTokenBalances":[{"accountIndex":4,"mint":"ZTgXiGpKZjEopH1mSqZ1GjY8k9G6dQaJoCm8iUkab7V","owner":"9C9ezHVSSpMrKAmqZa74jpUUxbjUBtcKUUYn8z9DDqqh","programId":"TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA","uiTokenAmount":{"amount":"10000000","decimals":6,"uiAmount":10.0,"uiAmountString":"10"}}],"preBalances":[5006672783,2793266460,17568823140,0,0,0,3476817,0,1566000,0,1,0,2832720,5938070540,1141440,1009200,15367267856],"preTokenBalances":[],"rewards":[],"status":{"Ok":null}},"slot":491383584,"transaction":{"message":{"accountKeys":["4QbAt2CJ8QqCHeps2RmMjG1nWUCmPqjkEyYQMjrJaVtH","2EEYH6e1PtCdWzZaag9buJmDDS79gvrm1aQm9yEcgWdR","4sQLizYQ1ZJjc2doLqTQsk1Kj8XVS5uviJykpHNNMSi5","4VC5j3WgPU7TTF8YzgikNdpF8zwnGkqAjf9iWfA5xCi7","59oiUm1Anavheg38XWDDdv3GAjD4XYSUhQBY8qyxXnTc","5BT6kxRRU2jSVbvVdeEBAszUoCXTzUpHWeruS5kYkqz","5PZEHeEx9mhMNPneusoQvLunGDLgHAQcyGmnoT1jJCEk","9C9ezHVSSpMrKAmqZa74jpUUxbjUBtcKUUYn8z9DDqqh","FDxeNn8YT8DoWrJ5GzqNTW8rjx8cLFNFBgHpBTMifeYJ","ZTgXiGpKZjEopH1mSqZ1GjY8k9G6dQaJoCm8iUkab7V","11111111111111111111111111111111","5FRwYKUHLYoSq6uNgrjZ2sq436AAzPnv77fv9e7EiFPi","7QAS73zgRm7KMt85XMGWbWDnmhZR1UyTULhhxR254YYR","ATokenGPvbdGVxr1b2hvZbsiqW5xWH25efTNsLJA8knL","DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp","SysvarRent111111111111111111111111111111111","TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA"],"header":{"numReadonlySignedAccounts":0,"numReadonlyUnsignedAccounts":7,"numRequiredSignatures":1},"instructions":[{"accounts":[0,12,2,7,8,5,10,10,1,14,4,14,16,15,13,14,14,14,6,0,11,14,3,9],"data":"42AXCSarXAyth9mmjqkpxW8qtkNjcbj5KuupCERvjRCapw5ydhidmiLmMnypfiZFedPUhVNQyeLPfF17ZtqeBJUS83v6DJRgwFHuVMK1dDy4MFW7QhMwVxfJwuZ1hgv9TtXcmdmixaHukCq77ikrN37w3UR2P12aY9ERa5tXuQGkxXAenYcsNHQuw7y99mXT2icCjvcVd3yGRtWgvnbrd4Gf36pzqRpZkrNgKdJzvSDYgoarEXdPq6m3GjZxRkvsgQJ2sZTsKbNCeRrL5tdxTRgd7YZXqXVDJ4ShaYAKqLXBqMGf1LbynGK2G8HUriKn41xVEFK2Rg37E3dykeHSDS","programIdIndex":14,"stackHeight":1}],"recentBlockhash":"2CEDFZcp6d5ug1BgDjuNzMPRXqi4WX8QTZskNr9yJicY"},"signatures":["4ye6nTo4oKcEctDza44Zr7QAwHmqhH4qfeBkDjHqE2aFtgxuhdF9dfs1EmBbYiTfwXMvWUupJ592DQQQAGx5Abus"]},"transactionIndex":12,"version":"legacy"}`

func TestGatewayEventPayloads_Rejects(t *testing.T) {
	var tx solanarpc.GetTransactionResult
	require.NoError(t, json.Unmarshal([]byte(devnetFinalizeTx), &tx))

	t.Run("unparseable gateway address", func(t *testing.T) {
		assert.Nil(t, gatewayEventPayloads(&tx, "not-a-pubkey"))
	})

	t.Run("nil transaction and nil meta", func(t *testing.T) {
		assert.Nil(t, gatewayEventPayloads(nil, testGatewayProgram))
		assert.Nil(t, gatewayEventPayloads(&solanarpc.GetTransactionResult{}, testGatewayProgram))
	})

	t.Run("inner instruction that is not an event", func(t *testing.T) {
		// Same emitter, but the data carries no EVENT_IX_TAG, which is every
		// ordinary self-CPI the gateway makes.
		var plain solanarpc.GetTransactionResult
		require.NoError(t, json.Unmarshal([]byte(txWithRawInnerData(t, testGatewayProgram, []byte{1, 2, 3})), &plain))
		assert.Empty(t, gatewayEventPayloads(&plain, testGatewayProgram))
	})
}

// txWithRawInnerData renders a transaction whose inner instruction data is used
// verbatim, with no emit_cpi tag prepended.
func txWithRawInnerData(t *testing.T, emitter string, data []byte) string {
	t.Helper()
	return fmt.Sprintf(`{
		"slot": 100,
		"transaction": {
			"signatures": ["%s"],
			"message": {
				"header": {"numRequiredSignatures":1,"numReadonlySignedAccounts":0,"numReadonlyUnsignedAccounts":1},
				"accountKeys": ["%s","%s"],
				"recentBlockhash": "9WzDXwBbmkg8ZTbNMqUxvQRAyrZzDsGYdLVL9zYtAWWM",
				"instructions": []
			}
		},
		"meta": {
			"err": null,
			"logMessages": [],
			"innerInstructions": [
				{"index":0,"instructions":[{"programIdIndex":1,"accounts":[],"data":"%s","stackHeight":2}]}
			]
		}
	}`, mkSig(7).String(), testGatewayProgram, emitter, base58.Encode(data))
}

// Pinned against a real transaction because the layout is set by the deployed
// program, not by us. gas_fee - gas_used == gas_to_refund in this event, which
// is what makes the offsets self-checking.
func TestGatewayEventPayloads_RealDevnetTransaction(t *testing.T) {
	const gateway = "DJoFYDpgbTfxbXBv1QYhYGc9FK4J5FUKpYXAfSkHryXp"

	var tx solanarpc.GetTransactionResult
	require.NoError(t, json.Unmarshal([]byte(devnetFinalizeTx), &tx))

	require.NotNil(t, tx.Meta)
	for _, l := range tx.Meta.LogMessages {
		require.False(t, strings.HasPrefix(l, "Program data: "),
			"this transaction predates emit_cpi if it still logs event data")
	}

	payloads := gatewayEventPayloads(&tx, gateway)
	require.Len(t, payloads, 1, "the finalize emits exactly one gateway event")

	el := &EventListener{
		gatewayAddress: gateway,
		// sha256("event:UniversalTxFinalized")[:8], as emitted on chain.
		discriminatorToEventType: map[string]string{"b3409670758c9c25": EventTypeFinalizeUniversalTx},
		chainID:                  "solana:devnet",
		logger:                   zerolog.Nop(),
	}
	eventType := el.determineEventType(payloads[0])
	require.Equal(t, EventTypeFinalizeUniversalTx, eventType)

	event := ParseEvent(payloads[0], "sig", 42, 0, eventType, "solana:devnet", zerolog.Nop())
	require.NotNil(t, event)

	var payload common.OutboundEvent
	require.NoError(t, json.Unmarshal(event.EventData, &payload))
	assert.Equal(t, "5260057", payload.GasFeeUsed, "gas_used must come from offset 112, not from wrapper_address")
	assert.Equal(t, store.EventTypeOutbound, event.Type)
}

// A program that is not the gateway can put anything in its own inner
// instruction, discriminator included, so the emitter check is what makes the
// event trustworthy.
func TestGatewayEventPayloads_IgnoresForeignEmitter(t *testing.T) {
	var tx solanarpc.GetTransactionResult
	require.NoError(t, json.Unmarshal([]byte(devnetFinalizeTx), &tx))

	assert.Empty(t, gatewayEventPayloads(&tx, "11111111111111111111111111111111"),
		"an event emitted by another program must not be read as the gateway's")
}
