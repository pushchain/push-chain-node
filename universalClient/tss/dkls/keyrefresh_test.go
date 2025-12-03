package dkls

import (
	"strings"
	"testing"

	session "go-wrapper/go-dkls/sessions"
)

func TestNewKeyrefreshSession_Validation(t *testing.T) {
	// Create a valid setup for tests that need it (need at least 2 participants for threshold 2)
	participants := []string{"party1", "party2"}
	participantIDs := encodeParticipantIDs(participants)
	validSetup, err := session.DklsKeygenSetupMsgNew(2, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create setup: %v", err)
	}

	tests := []struct {
		name         string
		setupData    []byte
		partyID      string
		participants []string
		oldKeyshare  []byte
		wantErr      string
	}{
		{"nil setupData", nil, "party1", participants, []byte("keyshare"), "setupData is required"},
		{"empty setupData", []byte{}, "party1", participants, []byte("keyshare"), "setupData is required"},
		{"empty party ID", validSetup, "", participants, []byte("keyshare"), "party ID required"},
		{"empty participants", validSetup, "party1", []string{}, []byte("keyshare"), "participants required"},
		{"nil participants", validSetup, "party1", nil, []byte("keyshare"), "participants required"},
		{"empty old keyshare", validSetup, "party1", participants, []byte{}, "old keyshare required"},
		{"nil old keyshare", validSetup, "party1", participants, nil, "old keyshare required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKeyrefreshSession(tt.setupData, "test-event", tt.partyID, tt.participants, 2, tt.oldKeyshare)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestKeyrefreshSession_EndToEnd(t *testing.T) {
	// First generate a keyshare using keygen
	participants := []string{"party1", "party2"}
	threshold := 2

	participantIDs := encodeParticipantIDs(participants)
	setupData, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create keygen setup: %v", err)
	}

	keygenParty1, err := NewKeygenSession(setupData, "test-keygen", "party1", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen party1: %v", err)
	}

	keygenParty2, err := NewKeygenSession(setupData, "test-keygen", "party2", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen party2: %v", err)
	}

	// Run keygen to completion
	keygenSessions := map[string]Session{
		"party1": keygenParty1,
		"party2": keygenParty2,
	}
	keygenResults := runToCompletion(t, keygenSessions)

	// Get keyshares from both parties
	keygenParty1Result := keygenResults["party1"]
	keygenParty2Result := keygenResults["party2"]

	if len(keygenParty1Result.Keyshare) == 0 {
		t.Fatal("keygen party1 keyshare is empty")
	}
	if len(keygenParty2Result.Keyshare) == 0 {
		t.Fatal("keygen party2 keyshare is empty")
	}

	// Store original public key for verification
	originalPublicKey := keygenParty2Result.PublicKey

	// Each party uses their own keyshare for keyrefresh
	party1OldKeyshare := keygenParty1Result.Keyshare
	party2OldKeyshare := keygenParty2Result.Keyshare

	// Now test keyrefresh with the same setup structure (keyrefresh uses keygen setup)
	refreshSetup, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create keyrefresh setup: %v", err)
	}

	refreshParty1, err := NewKeyrefreshSession(refreshSetup, "test-keyrefresh", "party1", participants, threshold, party1OldKeyshare)
	if err != nil {
		t.Fatalf("failed to create keyrefresh party1: %v", err)
	}

	refreshParty2, err := NewKeyrefreshSession(refreshSetup, "test-keyrefresh", "party2", participants, threshold, party2OldKeyshare)
	if err != nil {
		t.Fatalf("failed to create keyrefresh party2: %v", err)
	}

	// Run keyrefresh to completion
	refreshSessions := map[string]Session{
		"party1": refreshParty1,
		"party2": refreshParty2,
	}
	refreshResults := runToCompletion(t, refreshSessions)

	// Verify new keyshare
	refreshResult := refreshResults["party1"]
	if len(refreshResult.Keyshare) == 0 {
		t.Error("keyrefresh keyshare is empty")
	}
	if refreshResult.Signature != nil {
		t.Error("keyrefresh should not return signature")
	}
	if len(refreshResult.Participants) != 2 {
		t.Errorf("expected 2 participants, got %d", len(refreshResult.Participants))
	}

	// KeyRefresh: produces new keyshare, but same public key as old
	// Verify public key remains the same
	if len(refreshResult.PublicKey) != len(originalPublicKey) {
		t.Errorf("public key length changed: got %d, want %d", len(refreshResult.PublicKey), len(originalPublicKey))
	}
	for i := range originalPublicKey {
		if refreshResult.PublicKey[i] != originalPublicKey[i] {
			t.Errorf("public key changed at index %d", i)
			break
		}
	}

	// Verify keyshare is different
	keyshareChanged := false
	if len(refreshResult.Keyshare) != len(party1OldKeyshare) {
		keyshareChanged = true
	} else {
		for i := range party1OldKeyshare {
			if refreshResult.Keyshare[i] != party1OldKeyshare[i] {
				keyshareChanged = true
				break
			}
		}
	}
	if !keyshareChanged {
		t.Error("keyshare did not change after keyrefresh")
	}
}
