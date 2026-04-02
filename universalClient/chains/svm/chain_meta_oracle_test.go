package svm

import (
	"context"
	"testing"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/rs/zerolog"
)

func TestNewChainMetaOracle_BasicFields(t *testing.T) {
	logger := zerolog.Nop()
	rpc := &RPCClient{}
	ps := &pushsigner.Signer{}

	oracle := NewChainMetaOracle(rpc, ps, "solana-mainnet", 60, 10, logger)

	if oracle == nil {
		t.Fatal("expected non-nil oracle")
	}
	if oracle.rpcClient != rpc {
		t.Error("rpcClient not set correctly")
	}
	if oracle.pushSigner != ps {
		t.Error("pushSigner not set correctly")
	}
	if oracle.chainID != "solana-mainnet" {
		t.Errorf("chainID = %q, want %q", oracle.chainID, "solana-mainnet")
	}
	if oracle.gasPriceIntervalSeconds != 60 {
		t.Errorf("gasPriceIntervalSeconds = %d, want 60", oracle.gasPriceIntervalSeconds)
	}
	if oracle.gasPriceMarkupPercent != 10 {
		t.Errorf("gasPriceMarkupPercent = %d, want 10", oracle.gasPriceMarkupPercent)
	}
	if oracle.stopCh == nil {
		t.Error("stopCh channel should be initialized")
	}
}

func TestNewChainMetaOracle_NilRPCClient(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, &pushsigner.Signer{}, "chain-1", 30, 5, logger)

	if oracle == nil {
		t.Fatal("expected non-nil oracle even with nil rpcClient")
	}
	if oracle.rpcClient != nil {
		t.Error("rpcClient should be nil")
	}
}

func TestNewChainMetaOracle_NilPushSigner(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(&RPCClient{}, nil, "chain-2", 30, 5, logger)

	if oracle == nil {
		t.Fatal("expected non-nil oracle even with nil pushSigner")
	}
	if oracle.pushSigner != nil {
		t.Error("pushSigner should be nil")
	}
}

func TestNewChainMetaOracle_BothNil(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-3", 15, 0, logger)

	if oracle == nil {
		t.Fatal("expected non-nil oracle with nil rpcClient and pushSigner")
	}
}

func TestNewChainMetaOracle_EmptyChainID(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "", 30, 5, logger)

	if oracle.chainID != "" {
		t.Errorf("chainID = %q, want empty string", oracle.chainID)
	}
}

func TestNewChainMetaOracle_ZeroInterval(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-4", 0, 5, logger)

	if oracle.gasPriceIntervalSeconds != 0 {
		t.Errorf("gasPriceIntervalSeconds = %d, want 0", oracle.gasPriceIntervalSeconds)
	}
}

func TestNewChainMetaOracle_NegativeInterval(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-5", -10, 5, logger)

	if oracle.gasPriceIntervalSeconds != -10 {
		t.Errorf("gasPriceIntervalSeconds = %d, want -10", oracle.gasPriceIntervalSeconds)
	}
}

func TestNewChainMetaOracle_ZeroMarkup(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-6", 30, 0, logger)

	if oracle.gasPriceMarkupPercent != 0 {
		t.Errorf("gasPriceMarkupPercent = %d, want 0", oracle.gasPriceMarkupPercent)
	}
}

func TestNewChainMetaOracle_NegativeMarkup(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-7", 30, -20, logger)

	if oracle.gasPriceMarkupPercent != -20 {
		t.Errorf("gasPriceMarkupPercent = %d, want -20", oracle.gasPriceMarkupPercent)
	}
}

func TestNewChainMetaOracle_LargeValues(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-8", 86400, 500, logger)

	if oracle.gasPriceIntervalSeconds != 86400 {
		t.Errorf("gasPriceIntervalSeconds = %d, want 86400", oracle.gasPriceIntervalSeconds)
	}
	if oracle.gasPriceMarkupPercent != 500 {
		t.Errorf("gasPriceMarkupPercent = %d, want 500", oracle.gasPriceMarkupPercent)
	}
}

func TestGetChainMetaOracleFetchInterval_Positive(t *testing.T) {
	oracle := &ChainMetaOracle{gasPriceIntervalSeconds: 45}
	got := oracle.getChainMetaOracleFetchInterval()
	want := 45 * time.Second

	if got != want {
		t.Errorf("interval = %v, want %v", got, want)
	}
}

func TestGetChainMetaOracleFetchInterval_One(t *testing.T) {
	oracle := &ChainMetaOracle{gasPriceIntervalSeconds: 1}
	got := oracle.getChainMetaOracleFetchInterval()
	want := 1 * time.Second

	if got != want {
		t.Errorf("interval = %v, want %v", got, want)
	}
}

func TestGetChainMetaOracleFetchInterval_Zero(t *testing.T) {
	oracle := &ChainMetaOracle{gasPriceIntervalSeconds: 0}
	got := oracle.getChainMetaOracleFetchInterval()
	want := 30 * time.Second

	if got != want {
		t.Errorf("interval = %v, want %v (default for zero)", got, want)
	}
}

func TestGetChainMetaOracleFetchInterval_Negative(t *testing.T) {
	oracle := &ChainMetaOracle{gasPriceIntervalSeconds: -5}
	got := oracle.getChainMetaOracleFetchInterval()
	want := 30 * time.Second

	if got != want {
		t.Errorf("interval = %v, want %v (default for negative)", got, want)
	}
}

func TestGetChainMetaOracleFetchInterval_Large(t *testing.T) {
	oracle := &ChainMetaOracle{gasPriceIntervalSeconds: 3600}
	got := oracle.getChainMetaOracleFetchInterval()
	want := 3600 * time.Second

	if got != want {
		t.Errorf("interval = %v, want %v", got, want)
	}
}

func TestStop_WithoutStart_NoPanic(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-stop", 30, 5, logger)

	// Stop without Start should not panic or deadlock.
	done := make(chan struct{})
	go func() {
		defer close(done)
		oracle.Stop()
	}()

	select {
	case <-done:
		// success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() without prior Start() should not block indefinitely")
	}
}

func TestStartStop_ContextCancellation(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-ctx", 30, 5, logger)

	ctx, cancel := context.WithCancel(context.Background())

	err := oracle.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Cancel the context, which should cause fetchAndVoteChainMeta to return.
	cancel()

	// Wait for the goroutine to finish via wg.
	done := make(chan struct{})
	go func() {
		defer close(done)
		oracle.wg.Wait()
	}()

	select {
	case <-done:
		// success – goroutine exited via context cancellation
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine did not exit after context cancellation")
	}
}

func TestStartStop_ViaStopMethod(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-stop-method", 30, 5, logger)

	ctx := context.Background()

	err := oracle.Start(ctx)
	if err != nil {
		t.Fatalf("Start returned error: %v", err)
	}

	// Give the goroutine a moment to enter the select loop.
	time.Sleep(50 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		defer close(done)
		oracle.Stop()
	}()

	select {
	case <-done:
		// success – goroutine exited via Stop()
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() did not complete in time")
	}
}

func TestNewChainMetaOracle_StopChannelIsOpen(t *testing.T) {
	logger := zerolog.Nop()
	oracle := NewChainMetaOracle(nil, nil, "chain-ch", 30, 5, logger)

	// The stopCh should be open (not closed) after construction.
	select {
	case <-oracle.stopCh:
		t.Error("stopCh should be open after construction, but it was closed")
	default:
		// expected – channel is open
	}
}

func TestNewChainMetaOracle_DifferentChainIDs(t *testing.T) {
	logger := zerolog.Nop()

	chainIDs := []string{
		"solana-mainnet",
		"solana-devnet",
		"SVM_DEVNET",
		"chain:custom:123",
		"a]very[strange-id",
	}

	for _, id := range chainIDs {
		oracle := NewChainMetaOracle(nil, nil, id, 30, 5, logger)
		if oracle.chainID != id {
			t.Errorf("chainID = %q, want %q", oracle.chainID, id)
		}
	}
}
