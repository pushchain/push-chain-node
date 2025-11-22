package dkls

import (
	"strings"
	"testing"

	session "go-wrapper/go-dkls/sessions"
)

func TestNewSignSession_Validation(t *testing.T) {
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
		keyshare     []byte
		messageHash  []byte
		wantErr      string
	}{
		{"nil setupData", nil, "party1", participants, []byte("keyshare"), []byte("hash"), "setupData is required"},
		{"empty setupData", []byte{}, "party1", participants, []byte("keyshare"), []byte("hash"), "setupData is required"},
		{"empty party ID", validSetup, "", participants, []byte("keyshare"), []byte("hash"), "party ID required"},
		{"empty participants", validSetup, "party1", []string{}, []byte("keyshare"), []byte("hash"), "participants required"},
		{"nil participants", validSetup, "party1", nil, []byte("keyshare"), []byte("hash"), "participants required"},
		{"empty keyshare", validSetup, "party1", participants, []byte{}, []byte("hash"), "keyshare required"},
		{"nil keyshare", validSetup, "party1", participants, nil, []byte("hash"), "keyshare required"},
		{"empty message hash", validSetup, "party1", participants, []byte("keyshare"), []byte{}, "message hash required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSignSession(tt.setupData, "test-event", tt.partyID, tt.participants, tt.keyshare, tt.messageHash, nil)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestSignSession_EndToEnd(t *testing.T) {
	// First generate a keyshare
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

	// Complete keygen - both parties must finish
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

	// Use each party's own keyshare
	coordKeyshare := keygenCoordResult.Keyshare
	party2Keyshare := keygenParty2Result.Keyshare
	// Message hash must be 32 bytes (SHA256)
	messageHash := make([]byte, 32)
	copy(messageHash, "test-message-hash-to-sign-32bytes")

	// For sign, we need to create setup with keyID extracted from keyshare
	// Since keyshare contains keyID, we use empty keyID (library will validate against keyshare)
	// In practice, the coordinator would extract keyID from keyshare before creating setup
	emptyKeyID := make([]byte, 32) // Empty keyID - library validates it matches keyshare
	signSetup, err := session.DklsSignSetupMsgNew(emptyKeyID, nil, messageHash, participantIDs)
	if err != nil {
		t.Fatalf("failed to create sign setup: %v", err)
	}

	// Test sign - each party uses their own keyshare
	signCoord, err := NewSignSession(signSetup, "test-sign", "party1", participants, coordKeyshare, messageHash, nil)
	if err != nil {
		t.Fatalf("failed to create sign coordinator: %v", err)
	}
	defer signCoord.Close()

	signParty2, err := NewSignSession(signSetup, "test-sign", "party2", participants, party2Keyshare, messageHash, nil)
	if err != nil {
		t.Fatalf("failed to create sign party2: %v", err)
	}
	defer signParty2.Close()

	// Run sign to completion - both parties must finish
	signCoordDone := false
	signParty2Done := false
	for i := 0; i < 100; i++ {
		if !signCoordDone {
			msgs, done, err := signCoord.Step()
			if err != nil {
				t.Fatalf("sign coordinator Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party2" {
					signParty2.InputMessage(msg.Data)
				}
			}
			if done {
				signCoordDone = true
			}
		}
		if !signParty2Done {
			msgs, done, err := signParty2.Step()
			if err != nil {
				t.Fatalf("sign party2 Step() error at step %d: %v", i, err)
			}
			for _, msg := range msgs {
				if msg.Receiver == "party1" {
					signCoord.InputMessage(msg.Data)
				}
			}
			if done {
				signParty2Done = true
			}
		}
		if signCoordDone && signParty2Done {
			break
		}
	}

	if !signCoordDone || !signParty2Done {
		t.Fatal("sign did not complete for all parties")
	}

	// Verify signature
	result, err := signCoord.GetResult()
	if err != nil {
		t.Fatalf("sign GetResult() failed: %v", err)
	}
	if len(result.Signature) == 0 {
		t.Error("signature is empty")
	}
	if result.Keyshare != nil {
		t.Error("sign should not return keyshare")
	}
	if len(result.Participants) != 2 {
		t.Errorf("expected 2 participants, got %d", len(result.Participants))
	}
}
