package evm

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
)

// TestExecuteWithFailover_ConcurrentRotation verifies F-2026-16960 is fixed:
// under concurrent load, every caller must visit every endpoint exactly once,
// even when all endpoints fail (forcing the loop to run to completion).
func TestExecuteWithFailover_ConcurrentRotation(t *testing.T) {
	const numEndpoints = 3
	const numGoroutines = 200

	clients := make([]*ethclient.Client, numEndpoints)
	indexOf := make(map[*ethclient.Client]int, numEndpoints)
	for i := range clients {
		clients[i] = &ethclient.Client{}
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
			_ = rc.executeWithFailover(context.Background(), "test", func(c *ethclient.Client) error {
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

	clients := make([]*ethclient.Client, numEndpoints)
	indexOf := make(map[*ethclient.Client]int, numEndpoints)
	for i := range clients {
		clients[i] = &ethclient.Client{}
		indexOf[clients[i]] = i
	}

	rc := &RPCClient{clients: clients, logger: zerolog.Nop()}

	firstAttempts := make([]int, 0, numCalls)
	for i := 0; i < numCalls; i++ {
		_ = rc.executeWithFailover(context.Background(), "test", func(c *ethclient.Client) error {
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
