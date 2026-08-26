package common

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfirmationDepth(t *testing.T) {
	tests := []struct {
		name      string
		latest    uint64
		tx        uint64
		wantDepth uint64
		wantOK    bool
	}{
		{"latest greater than tx", 110, 100, 11, true},
		{"latest equals tx (inclusion block)", 100, 100, 1, true},
		{"latest one below tx (skew)", 99, 100, 0, false},
		{"latest far below tx (skew)", 1, math.MaxUint64, 0, false},
		{"no underflow to near-2^64", 0, 1, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			depth, ok := ConfirmationDepth(tc.latest, tc.tx)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantDepth, depth)
		})
	}
}

// TestConfirmationDepth_SkewNeverSatisfiesThreshold guards the exact finding:
// a transaction one block ahead of the observed tip must not produce a depth
// that clears a realistic confirmation threshold.
func TestConfirmationDepth_SkewNeverSatisfiesThreshold(t *testing.T) {
	const threshold = uint64(12)
	depth, ok := ConfirmationDepth(500, 501)
	assert.False(t, ok, "skewed read must be flagged not-ok")
	assert.False(t, depth >= threshold, "skewed depth must not satisfy threshold")
}
