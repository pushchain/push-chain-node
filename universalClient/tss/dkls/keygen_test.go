package dkls

import (
	"strings"
	"testing"

	session "go-wrapper/go-dkls/sessions"
)

func TestNewKeygenSession_Validation(t *testing.T) {
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
		wantErr      string
	}{
		{"nil setupData", nil, "party1", participants, "setupData is required"},
		{"empty setupData", []byte{}, "party1", participants, "setupData is required"},
		{"empty party ID", validSetup, "", participants, "party ID required"},
		{"empty participants", validSetup, "party1", []string{}, "participants required"},
		{"nil participants", validSetup, "party1", nil, "participants required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKeygenSession(tt.setupData, "test-event", tt.partyID, tt.participants, 2)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestKeygenSession_EndToEnd(t *testing.T) {
	participants := []string{"party1", "party2", "party3"}
	threshold := 2

	// Create setup message
	participantIDs := encodeParticipantIDs(participants)
	setupData, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create setup: %v", err)
	}

	// Create all sessions with the same setup
	party1Session, err := NewKeygenSession(setupData, "test-event", "party1", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create party1 session: %v", err)
	}

	party2Session, err := NewKeygenSession(setupData, "test-event", "party2", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create party2 session: %v", err)
	}

	party3Session, err := NewKeygenSession(setupData, "test-event", "party3", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create party3 session: %v", err)
	}

	// Run protocol to completion
	keygenSessions := map[string]Session{
		"party1": party1Session,
		"party2": party2Session,
		"party3": party3Session,
	}
	results := runToCompletion(t, keygenSessions)

	// Verify party1 got keyshare
	result := results["party1"]

	if len(result.Keyshare) == 0 {
		t.Error("party1 keyshare is empty")
	}
	if result.Signature != nil {
		t.Error("keygen should not return signature")
	}
	if len(result.Participants) != 3 {
		t.Errorf("expected 3 participants, got %d", len(result.Participants))
	}
}
