package svm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	solanarpc "github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/universalClient/db"
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
