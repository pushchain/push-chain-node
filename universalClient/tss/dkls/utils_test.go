package dkls

import (
	"bytes"
	"crypto/sha256"
	"testing"

	session "go-wrapper/go-dkls/sessions"
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

// The setup blob is what DKLS actually runs on, so these decoders are what let a
// follower bind it to the values it validated separately. Both must report what
// the blob really contains, and must error rather than guess on a malformed one.

func TestSetupMessageHash(t *testing.T) {
	participantIDs := encodeParticipantIDs([]string{"party1", "party2"})
	keyID := make([]byte, 32)

	legitHash := make([]byte, 32)
	copy(legitHash, "legitimate-outbound-hash-32bytes")
	attackerHash := make([]byte, 32)
	copy(attackerHash, "attacker-chosen-vault-call-digest")

	t.Run("returns the hash embedded in the setup", func(t *testing.T) {
		setup, err := session.DklsSignSetupMsgNew(keyID, nil, legitHash, participantIDs)
		if err != nil {
			t.Fatalf("failed to build sign setup: %v", err)
		}
		got, err := SetupMessageHash(setup)
		if err != nil {
			t.Fatalf("SetupMessageHash() error = %v", err)
		}
		if !bytes.Equal(got, legitHash) {
			t.Errorf("SetupMessageHash() = %x, want %x", got, legitHash)
		}
	})

	// A substituted setup must report the hash it really signs, which is what
	// makes the mismatch detectable.
	t.Run("substituted setup reports the attacker hash", func(t *testing.T) {
		setup, err := session.DklsSignSetupMsgNew(keyID, nil, attackerHash, participantIDs)
		if err != nil {
			t.Fatalf("failed to build sign setup: %v", err)
		}
		got, err := SetupMessageHash(setup)
		if err != nil {
			t.Fatalf("SetupMessageHash() error = %v", err)
		}
		if bytes.Equal(got, legitHash) {
			t.Fatal("substituted setup must not report the legitimate hash")
		}
		if !bytes.Equal(got, attackerHash) {
			t.Errorf("SetupMessageHash() = %x, want %x", got, attackerHash)
		}
	})

	t.Run("errors on empty and malformed setup", func(t *testing.T) {
		if _, err := SetupMessageHash(nil); err == nil {
			t.Error("SetupMessageHash(nil) should error")
		}
		if _, err := SetupMessageHash([]byte("not-a-dkls-setup")); err == nil {
			t.Error("SetupMessageHash(malformed) should error")
		}
	})
}

func TestSetupParticipants(t *testing.T) {
	t.Run("returns the participants in index order", func(t *testing.T) {
		want := []string{"alice", "bob", "carol"}
		setup, err := session.DklsKeygenSetupMsgNew(2, nil, encodeParticipantIDs(want))
		if err != nil {
			t.Fatalf("failed to build keygen setup: %v", err)
		}
		got, err := SetupParticipants(setup)
		if err != nil {
			t.Fatalf("SetupParticipants() error = %v", err)
		}
		if len(got) != len(want) {
			t.Fatalf("SetupParticipants() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("participant %d = %q, want %q", i, got[i], want[i])
			}
		}
	})

	// Enumeration terminates on the first empty name rather than an error, which
	// is the contract this relies on to find the end of the list.
	t.Run("terminates at the end of a two party list", func(t *testing.T) {
		want := []string{"first", "second"}
		setup, err := session.DklsKeygenSetupMsgNew(2, nil, encodeParticipantIDs(want))
		if err != nil {
			t.Fatalf("failed to build keygen setup: %v", err)
		}
		got, err := SetupParticipants(setup)
		if err != nil {
			t.Fatalf("SetupParticipants() error = %v", err)
		}
		if len(got) != 2 || got[0] != "first" || got[1] != "second" {
			t.Errorf("SetupParticipants() = %v, want %v", got, want)
		}
	})

	t.Run("errors on empty and malformed setup", func(t *testing.T) {
		if _, err := SetupParticipants(nil); err == nil {
			t.Error("SetupParticipants(nil) should error")
		}
		if _, err := SetupParticipants([]byte("not-a-dkls-setup")); err == nil {
			t.Error("SetupParticipants(malformed) should error")
		}
	})
}

// Pins the setup TLV layout this package parses directly. If the library
// changes its encoding, this fails loudly instead of SetupThreshold silently
// reading the wrong byte.
func TestSetupThreshold(t *testing.T) {
	participants := []string{"alice", "bob", "carol"}

	t.Run("reads the embedded keygen threshold", func(t *testing.T) {
		for _, want := range []int{2, 3} {
			setup, err := session.DklsKeygenSetupMsgNew(want, nil, encodeParticipantIDs(participants))
			if err != nil {
				t.Fatalf("failed to build keygen setup with threshold %d: %v", want, err)
			}
			got, err := SetupThreshold(setup)
			if err != nil {
				t.Fatalf("SetupThreshold() error = %v", err)
			}
			if got != want {
				t.Errorf("SetupThreshold() = %d, want %d", got, want)
			}
		}
	})

	// A downgraded setup must report the weaker threshold it really carries,
	// which is what makes the mismatch detectable.
	t.Run("downgraded setup reports the weaker threshold", func(t *testing.T) {
		setup, err := session.DklsKeygenSetupMsgNew(2, nil, encodeParticipantIDs(participants))
		if err != nil {
			t.Fatalf("failed to build keygen setup: %v", err)
		}
		got, err := SetupThreshold(setup)
		if err != nil {
			t.Fatalf("SetupThreshold() error = %v", err)
		}
		if got == 3 {
			t.Fatal("downgraded setup must not report the expected threshold")
		}
		if got != 2 {
			t.Errorf("SetupThreshold() = %d, want 2", got)
		}
	})

	t.Run("errors on short, malformed and thresholdless setups", func(t *testing.T) {
		if _, err := SetupThreshold(nil); err == nil {
			t.Error("SetupThreshold(nil) should error")
		}
		if _, err := SetupThreshold([]byte("too-short")); err == nil {
			t.Error("SetupThreshold(short) should error")
		}
		// Header present but no tags at all.
		if _, err := SetupThreshold(make([]byte, setupHeaderSize)); err == nil {
			t.Error("SetupThreshold(no tags) should error")
		}
	})
}

// Cross-protocol matrix over the three setup shapes the coordinator builds:
// keygen (shared with keyrefresh), quorumchange, and sign (shared with fund
// migration). Pins what each decoder returns for each shape, so a rebuilt DKLS
// library that changes the encoding fails here rather than in production.
func TestSetupDecoders_AllProtocols(t *testing.T) {
	participants := []string{"party1", "party2", "party3"}
	ids := encodeParticipantIDs(participants)
	const threshold = 2

	messageHash := make([]byte, 32)
	copy(messageHash, "outbound-signing-hash-32-bytes!!")

	// keygen, also used verbatim for keyrefresh
	keygenSetup, err := session.DklsKeygenSetupMsgNew(threshold, nil, ids)
	if err != nil {
		t.Fatalf("failed to build keygen setup: %v", err)
	}

	// sign, also used for fund migration
	signSetup, err := session.DklsSignSetupMsgNew(make([]byte, 32), nil, messageHash, ids)
	if err != nil {
		t.Fatalf("failed to build sign setup: %v", err)
	}

	// quorumchange needs a real keyshare, so run a keygen to completion first
	sessions := map[string]Session{}
	for _, p := range participants {
		sess, err := NewKeygenSession(keygenSetup, "matrix", p, participants, threshold)
		if err != nil {
			t.Fatalf("failed to create keygen session for %s: %v", p, err)
		}
		sessions[p] = sess
	}
	keyshare := runToCompletion(t, sessions)["party1"].Keyshare
	handle, err := session.DklsKeyshareFromBytes(keyshare)
	if err != nil {
		t.Fatalf("failed to load keyshare: %v", err)
	}
	defer session.DklsKeyshareFree(handle)

	qcSetup, err := session.DklsQcSetupMsgNew(handle, threshold, participants, []int{0, 1, 2}, []int{0, 1, 2})
	if err != nil {
		t.Fatalf("failed to build QC setup: %v", err)
	}

	shapes := []struct {
		name         string
		setup        []byte
		wantHash     []byte // nil means the shape carries no message
		hasThreshold bool
	}{
		{"keygen and keyrefresh", keygenSetup, nil, true},
		{"quorumchange", qcSetup, nil, true},
		{"sign and fund migration", signSetup, messageHash, false},
	}

	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			// Participants are bound for every protocol, so every shape must decode them.
			got, err := SetupParticipants(sh.setup)
			if err != nil {
				t.Fatalf("SetupParticipants() error = %v", err)
			}
			if len(got) != len(participants) {
				t.Fatalf("SetupParticipants() = %v, want %v", got, participants)
			}
			for i := range participants {
				if got[i] != participants[i] {
					t.Errorf("participant %d = %q, want %q", i, got[i], participants[i])
				}
			}

			gotThreshold, thresholdErr := SetupThreshold(sh.setup)
			if sh.hasThreshold {
				if thresholdErr != nil {
					t.Fatalf("SetupThreshold() error = %v", thresholdErr)
				}
				if gotThreshold != threshold {
					t.Errorf("SetupThreshold() = %d, want %d", gotThreshold, threshold)
				}
			} else if thresholdErr == nil {
				// Sign setups carry no threshold. Erroring is what stops a bogus
				// value being read out of unrelated bytes.
				t.Errorf("SetupThreshold() on a sign setup returned %d, want an error", gotThreshold)
			}

			gotHash, hashErr := SetupMessageHash(sh.setup)
			if hashErr != nil {
				t.Fatalf("SetupMessageHash() error = %v", hashErr)
			}
			if sh.wantHash == nil {
				// Shapes without a message report an empty hash rather than an
				// error, so a non-sign setup can never satisfy the hash binding.
				if len(gotHash) != 0 {
					t.Errorf("SetupMessageHash() = %x, want empty", gotHash)
				}
			} else if !bytes.Equal(gotHash, sh.wantHash) {
				t.Errorf("SetupMessageHash() = %x, want %x", gotHash, sh.wantHash)
			}
		})
	}
}
