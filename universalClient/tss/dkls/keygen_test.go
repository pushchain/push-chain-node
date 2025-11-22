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

	// Create setup message (coordinator would do this)
	participantIDs := encodeParticipantIDs(participants)
	setupData, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create setup: %v", err)
	}

	// Create all sessions with the same setup
	coordSession, err := NewKeygenSession(setupData, "test-event", "party1", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create coordinator session: %v", err)
	}
	defer coordSession.Close()

	party2Session, err := NewKeygenSession(setupData, "test-event", "party2", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create party2 session: %v", err)
	}
	defer party2Session.Close()

	party3Session, err := NewKeygenSession(setupData, "test-event", "party3", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create party3 session: %v", err)
	}
	defer party3Session.Close()

	// Run protocol to completion
	maxSteps := 100
	coordDone := false
	party2Done := false
	party3Done := false

	for step := 0; step < maxSteps; step++ {
		// Coordinator step
		if !coordDone {
			msgs, done, err := coordSession.Step()
			if err != nil {
				t.Fatalf("coordinator Step() error at step %d: %v", step, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party2" {
					party2Session.InputMessage(msg.Data)
				} else if msg.Receiver == "party3" {
					party3Session.InputMessage(msg.Data)
				}
			}
			if done {
				coordDone = true
			}
		}

		// Party2 step
		if !party2Done {
			msgs, done, err := party2Session.Step()
			if err != nil {
				t.Fatalf("party2 Step() error at step %d: %v", step, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party1" {
					coordSession.InputMessage(msg.Data)
				} else if msg.Receiver == "party3" {
					party3Session.InputMessage(msg.Data)
				}
			}
			if done {
				party2Done = true
			}
		}

		// Party3 step
		if !party3Done {
			msgs, done, err := party3Session.Step()
			if err != nil {
				t.Fatalf("party3 Step() error at step %d: %v", step, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party1" {
					coordSession.InputMessage(msg.Data)
				} else if msg.Receiver == "party2" {
					party2Session.InputMessage(msg.Data)
				}
			}
			if done {
				party3Done = true
			}
		}

		if coordDone && party2Done && party3Done {
			break
		}
	}

	if !coordDone {
		t.Fatal("coordinator did not finish")
	}

	// Verify coordinator got keyshare
	result, err := coordSession.GetResult()

	if err != nil {
		t.Fatalf("coordinator GetResult() failed: %v", err)
	}
	if len(result.Keyshare) == 0 {
		t.Error("coordinator keyshare is empty")
	}
	if result.Signature != nil {
		t.Error("keygen should not return signature")
	}
	if len(result.Participants) != 3 {
		t.Errorf("expected 3 participants, got %d", len(result.Participants))
	}
}
