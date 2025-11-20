package dkls

import (
	"strings"
	"testing"

	session "go-wrapper/go-dkls/sessions"
)

func TestNewKeyrefreshSession_Validation(t *testing.T) {
	tests := []struct {
		name         string
		partyID      string
		participants []string
		oldKeyshare  []byte
		wantErr      string
	}{
		{"empty party ID", "", []string{"party1"}, []byte("keyshare"), "party ID required"},
		{"empty participants", "party1", []string{}, []byte("keyshare"), "participants required"},
		{"nil participants", "party1", nil, []byte("keyshare"), "participants required"},
		{"empty old keyshare", "party1", []string{"party1"}, []byte{}, "old keyshare required"},
		{"nil old keyshare", "party1", []string{"party1"}, nil, "old keyshare required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewKeyrefreshSession(nil, "test-event", tt.partyID, tt.participants, 2, tt.oldKeyshare)
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

	keygenCoord, err := NewKeygenSession(setupData, "test-keygen", "party1", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen coordinator: %v", err)
	}
	defer keygenCoord.Close()

	keygenParty2, err := NewKeygenSession(setupData, "test-keygen", "party2", participants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen party2: %v", err)
	}
	defer keygenParty2.Close()

	// Complete keygen to get keyshare - both parties must finish
	keygenCoordDone := false
	keygenParty2Done := false
	for i := 0; i < 100; i++ {
		if !keygenCoordDone {
			msgs, done, err := keygenCoord.Step()
			if err != nil {
				t.Fatalf("keygen coordinator Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party2" {
					keygenParty2.InputMessage(msg.Data)
				}
			}
			if done {
				keygenCoordDone = true
			}
		}
		if !keygenParty2Done {
			msgs, done, err := keygenParty2.Step()
			if err != nil {
				t.Fatalf("keygen party2 Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party1" {
					keygenCoord.InputMessage(msg.Data)
				}
			}
			if done {
				keygenParty2Done = true
			}
		}
		if keygenCoordDone && keygenParty2Done {
			break
		}
	}

	if !keygenCoordDone || !keygenParty2Done {
		t.Fatal("keygen did not complete for all parties")
	}

	// Get keyshares from both parties
	keygenCoordResult, err := keygenCoord.GetResult()
	if err != nil {
		t.Fatalf("keygen coordinator GetResult() failed: %v", err)
	}
	if len(keygenCoordResult.Keyshare) == 0 {
		t.Fatal("keygen coordinator keyshare is empty")
	}

	keygenParty2Result, err := keygenParty2.GetResult()
	if err != nil {
		t.Fatalf("keygen party2 GetResult() failed: %v", err)
	}
	if len(keygenParty2Result.Keyshare) == 0 {
		t.Fatal("keygen party2 keyshare is empty")
	}

	// Each party uses their own keyshare for keyrefresh
	coordOldKeyshare := keygenCoordResult.Keyshare
	party2OldKeyshare := keygenParty2Result.Keyshare

	// Now test keyrefresh with the same setup structure (keyrefresh uses keygen setup)
	// Use the same setup as keygen (nil keyID for auto-generation)
	refreshSetup, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create keyrefresh setup: %v", err)
	}

	refreshCoord, err := NewKeyrefreshSession(refreshSetup, "test-keyrefresh", "party1", participants, threshold, coordOldKeyshare)
	if err != nil {
		t.Fatalf("failed to create keyrefresh coordinator: %v", err)
	}
	defer refreshCoord.Close()

	refreshParty2, err := NewKeyrefreshSession(refreshSetup, "test-keyrefresh", "party2", participants, threshold, party2OldKeyshare)
	if err != nil {
		t.Fatalf("failed to create keyrefresh party2: %v", err)
	}
	defer refreshParty2.Close()

	// Run keyrefresh to completion - both parties must finish
	refreshCoordDone := false
	refreshParty2Done := false
	for i := 0; i < 100; i++ {
		if !refreshCoordDone {
			msgs, done, err := refreshCoord.Step()
			if err != nil {
				t.Fatalf("keyrefresh coordinator Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party2" {
					refreshParty2.InputMessage(msg.Data)
				}
			}
			if done {
				refreshCoordDone = true
			}
		}
		if !refreshParty2Done {
			msgs, done, err := refreshParty2.Step()
			if err != nil {
				t.Fatalf("keyrefresh party2 Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party1" {
					refreshCoord.InputMessage(msg.Data)
				}
			}
			if done {
				refreshParty2Done = true
			}
		}
		if refreshCoordDone && refreshParty2Done {
			break
		}
	}

	if !refreshCoordDone || !refreshParty2Done {
		t.Fatal("keyrefresh did not complete for all parties")
	}

	// Verify new keyshare
	result, err := refreshCoord.GetResult()
	if err != nil {
		t.Fatalf("keyrefresh GetResult() failed: %v", err)
	}
	if len(result.Keyshare) == 0 {
		t.Error("keyrefresh keyshare is empty")
	}
	if result.Signature != nil {
		t.Error("keyrefresh should not return signature")
	}
	if len(result.Participants) != 2 {
		t.Errorf("expected 2 participants, got %d", len(result.Participants))
	}
}
