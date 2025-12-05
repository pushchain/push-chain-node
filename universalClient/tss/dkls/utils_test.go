package dkls

import (
	"crypto/sha256"
	"testing"
)

func TestDeriveKeyID(t *testing.T) {
	tests := []struct {
		keyID string
	}{
		{""},
		{"test-key"},
		{"very-long-key-id"},
	}

	for _, tt := range tests {
		t.Run(tt.keyID, func(t *testing.T) {
			result := deriveKeyID(tt.keyID)
			expected := sha256.Sum256([]byte(tt.keyID))
			if len(result) != 32 {
				t.Errorf("expected length 32, got %d", len(result))
			}
			for i := range expected {
				if result[i] != expected[i] {
					t.Errorf("mismatch at index %d", i)
					break
				}
			}
		})
	}
}

func TestEncodeParticipantIDs(t *testing.T) {
	tests := []struct {
		name         string
		participants []string
		wantNulls    int
	}{
		{"single", []string{"party1"}, 0},
		{"two", []string{"party1", "party2"}, 1},
		{"three", []string{"a", "b", "c"}, 2},
		{"empty", []string{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := encodeParticipantIDs(tt.participants)
			nullCount := 0
			for _, b := range result {
				if b == 0 {
					nullCount++
				}
			}
			if nullCount != tt.wantNulls {
				t.Errorf("expected %d null separators, got %d", tt.wantNulls, nullCount)
			}
		})
	}
}
