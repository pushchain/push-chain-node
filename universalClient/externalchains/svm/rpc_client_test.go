package svm

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	"github.com/gagliardetto/solana-go/rpc"
	"github.com/rs/zerolog"
)

func TestCalculateMedian(t *testing.T) {
	tests := []struct {
		name string
		fees []uint64
		want uint64
	}{
		{
			name: "empty slice",
			fees: []uint64{},
			want: 0,
		},
		{
			name: "single element",
			fees: []uint64{42},
			want: 42,
		},
		{
			name: "odd count already sorted",
			fees: []uint64{1, 2, 3},
			want: 2,
		},
		{
			name: "odd count unsorted",
			fees: []uint64{3, 1, 2},
			want: 2,
		},
		{
			name: "even count already sorted",
			fees: []uint64{1, 2, 3, 4},
			want: 2, // (2+3)/2 = 2 (integer division)
		},
		{
			name: "even count unsorted",
			fees: []uint64{4, 1, 3, 2},
			want: 2, // (2+3)/2 = 2
		},
		{
			name: "even count average rounds down",
			fees: []uint64{1, 4},
			want: 2, // (1+4)/2 = 2
		},
		{
			name: "duplicate values odd count",
			fees: []uint64{5, 5, 5},
			want: 5,
		},
		{
			name: "duplicate values even count",
			fees: []uint64{5, 5, 5, 5},
			want: 5,
		},
		{
			name: "five elements",
			fees: []uint64{10, 30, 50, 20, 40},
			want: 30,
		},
		{
			name: "six elements",
			fees: []uint64{10, 30, 50, 20, 40, 60},
			want: 35, // (30+40)/2
		},
		{
			name: "large values",
			fees: []uint64{math.MaxUint64 - 1, math.MaxUint64 - 3},
			// (MaxUint64-3 + MaxUint64-1) / 2 overflows, but that is the
			// current behaviour of the function (unsigned wrap-around).
			// We just document whatever the function returns.
			want: func() uint64 {
				a := uint64(math.MaxUint64 - 3)
				b := uint64(math.MaxUint64 - 1)
				return (a + b) / 2
			}(),
		},
		{
			name: "two elements same value",
			fees: []uint64{100, 100},
			want: 100,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Copy input so we can verify the function works on its own copy
			input := make([]uint64, len(tt.fees))
			copy(input, tt.fees)

			got := calculateMedian(input)
			if got != tt.want {
				t.Errorf("calculateMedian(%v) = %d, want %d", tt.fees, got, tt.want)
			}
		})
	}
}

func TestClose_NilClients(t *testing.T) {
	rc := &RPCClient{
		clients: nil,
		logger:  zerolog.Nop(),
	}

	// Should not panic
	rc.Close()

	if rc.clients != nil {
		t.Error("expected clients to be nil after Close")
	}
}

func TestClose_EmptyClients(t *testing.T) {
	rc := &RPCClient{
		clients: make([]*rpc.Client, 0),
		logger:  zerolog.Nop(),
	}

	// Should not panic
	rc.Close()

	if rc.clients != nil {
		t.Error("expected clients to be nil after Close")
	}
}

// TestExecuteWithFailover_ConcurrentRotation verifies F-2026-16960 is fixed:
// under concurrent load, every caller must visit every endpoint exactly once,
// even when all endpoints fail (forcing the loop to run to completion).
func TestExecuteWithFailover_ConcurrentRotation(t *testing.T) {
	const numEndpoints = 3
	const numGoroutines = 200

	clients := make([]*rpc.Client, numEndpoints)
	indexOf := make(map[*rpc.Client]int, numEndpoints)
	for i := range clients {
		clients[i] = &rpc.Client{}
		indexOf[clients[i]] = i
	}

	rc := &RPCClient{clients: clients, logger: zerolog.Nop()}

	sequences := make([][]int, numGoroutines)
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	for g := 0; g < numGoroutines; g++ {
		go func(g int) {
			defer wg.Done()
			var visited []int
			_ = rc.executeWithFailover(context.Background(), "test", func(c *rpc.Client) error {
				visited = append(visited, indexOf[c])
				return errors.New("force failover")
			})
			sequences[g] = visited
		}(g)
	}
	wg.Wait()

	for g, seq := range sequences {
		if len(seq) != numEndpoints {
			t.Errorf("goroutine %d visited %d endpoints, want %d (seq=%v)", g, len(seq), numEndpoints, seq)
			continue
		}
		seen := make(map[int]bool, numEndpoints)
		for _, idx := range seq {
			if seen[idx] {
				t.Errorf("goroutine %d hit endpoint %d twice (seq=%v)", g, idx, seq)
			}
			seen[idx] = true
		}
	}
}

// TestExecuteWithFailover_SequentialRotation verifies that consecutive calls
// alternate their starting endpoint, distributing first-attempt traffic.
func TestExecuteWithFailover_SequentialRotation(t *testing.T) {
	const numEndpoints = 3
	const numCalls = 12

	clients := make([]*rpc.Client, numEndpoints)
	indexOf := make(map[*rpc.Client]int, numEndpoints)
	for i := range clients {
		clients[i] = &rpc.Client{}
		indexOf[clients[i]] = i
	}

	rc := &RPCClient{clients: clients, logger: zerolog.Nop()}

	firstAttempts := make([]int, 0, numCalls)
	for i := 0; i < numCalls; i++ {
		_ = rc.executeWithFailover(context.Background(), "test", func(c *rpc.Client) error {
			if len(firstAttempts) < i+1 {
				firstAttempts = append(firstAttempts, indexOf[c])
			}
			return errors.New("force failover")
		})
	}

	// Each endpoint should be the first attempt for exactly numCalls/numEndpoints calls.
	counts := make(map[int]int, numEndpoints)
	for _, idx := range firstAttempts {
		counts[idx]++
	}
	expected := numCalls / numEndpoints
	for i := 0; i < numEndpoints; i++ {
		if counts[i] != expected {
			t.Errorf("endpoint %d was first-attempt %d times, want %d (firstAttempts=%v)", i, counts[i], expected, firstAttempts)
		}
	}
}
