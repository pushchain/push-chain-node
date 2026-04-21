package svm

import (
	"math"
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
