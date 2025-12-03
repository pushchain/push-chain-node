package dkls

import (
	"strings"
	"testing"

	session "go-wrapper/go-dkls/sessions"
)

func TestNewQuorumChangeSession_Validation(t *testing.T) {
	// First create a keygen to get old shares for testing
	keygenParticipants := []string{"party1", "party2", "party3"}
	keygenParticipantIDs := encodeParticipantIDs(keygenParticipants)
	keygenSetup, err := session.DklsKeygenSetupMsgNew(3, nil, keygenParticipantIDs)
	if err != nil {
		t.Fatalf("failed to create keygen setup: %v", err)
	}

	// Create keygen sessions
	kg1, err := NewKeygenSession(keygenSetup, "keygen", "party1", keygenParticipants, 3)
	if err != nil {
		t.Fatalf("failed to create keygen session 1: %v", err)
	}

	kg2, err := NewKeygenSession(keygenSetup, "keygen", "party2", keygenParticipants, 3)
	if err != nil {
		t.Fatalf("failed to create keygen session 2: %v", err)
	}

	kg3, err := NewKeygenSession(keygenSetup, "keygen", "party3", keygenParticipants, 3)
	if err != nil {
		t.Fatalf("failed to create keygen session 3: %v", err)
	}

	// Run keygen to completion
	keygenSessions := map[string]Session{
		"party1": kg1,
		"party2": kg2,
		"party3": kg3,
	}
	keygenResults := runToCompletion(t, keygenSessions)

	kg1Result := keygenResults["party1"]

	// Create QC setup for validation tests
	ids := []string{"party1", "party2", "party3", "party4"}
	oldKeyshareHandle, err := session.DklsKeyshareFromBytes(kg1Result.Keyshare)
	if err != nil {
		t.Fatalf("failed to load old keyshare: %v", err)
	}
	defer session.DklsKeyshareFree(oldKeyshareHandle)

	setup, err := session.DklsQcSetupMsgNew(
		oldKeyshareHandle,
		3,
		ids,
		[]int{0, 1, 2},    // old parties: party1, party2, party3 (indices in ids)
		[]int{0, 1, 2, 3}, // new parties: all parties in new quorum (party1, party2, party3, party4)
	)
	if err != nil {
		t.Fatalf("failed to create QC setup: %v", err)
	}

	tests := []struct {
		name         string
		setupData    []byte
		partyID      string
		participants []string
		oldKeyshare  []byte
		wantErr      string
	}{
		{"nil setupData", nil, "party1", ids, kg1Result.Keyshare, "setupData is required"},
		{"empty setupData", []byte{}, "party1", ids, kg1Result.Keyshare, "setupData is required"},
		{"empty party ID", setup, "", ids, kg1Result.Keyshare, "party ID required"},
		{"empty participants", setup, "party1", []string{}, kg1Result.Keyshare, "participants required"},
		{"nil participants", setup, "party1", nil, kg1Result.Keyshare, "participants required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQuorumChangeSession(tt.setupData, "test-event", tt.partyID, tt.participants, 3, tt.oldKeyshare)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("expected error containing %q, got %v", tt.wantErr, err)
			}
		})
	}
}

func TestQuorumChangeSession_EndToEnd(t *testing.T) {
	// Start with 3 nodes with threshold 3
	initialParticipants := []string{"party1", "party2", "party3"}
	threshold := 3

	participantIDs := encodeParticipantIDs(initialParticipants)
	keygenSetup, err := session.DklsKeygenSetupMsgNew(threshold, nil, participantIDs)
	if err != nil {
		t.Fatalf("failed to create keygen setup: %v", err)
	}

	// Create and run keygen sessions
	kg1, err := NewKeygenSession(keygenSetup, "keygen", "party1", initialParticipants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen session 1: %v", err)
	}

	kg2, err := NewKeygenSession(keygenSetup, "keygen", "party2", initialParticipants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen session 2: %v", err)
	}

	kg3, err := NewKeygenSession(keygenSetup, "keygen", "party3", initialParticipants, threshold)
	if err != nil {
		t.Fatalf("failed to create keygen session 3: %v", err)
	}

	// Run keygen to completion
	keygenSessions := map[string]Session{
		"party1": kg1,
		"party2": kg2,
		"party3": kg3,
	}
	keygenResults := runToCompletion(t, keygenSessions)

	kg1Result := keygenResults["party1"]
	kg2Result := keygenResults["party2"]
	kg3Result := keygenResults["party3"]

	if len(kg1Result.Keyshare) == 0 {
		t.Fatal("keygen party1 keyshare is empty")
	}
	if len(kg2Result.Keyshare) == 0 {
		t.Fatal("keygen party2 keyshare is empty")
	}
	if len(kg3Result.Keyshare) == 0 {
		t.Fatal("keygen party3 keyshare is empty")
	}

	// Store original keyID and public key for verification
	originalKeyID := kg1Result.KeyID
	originalPublicKey := kg1Result.PublicKey

	// Now add party4 with threshold 3 (4 nodes total, threshold 3)
	newParticipants := []string{"party1", "party2", "party3", "party4"}
	oldKeyshareHandle, err := session.DklsKeyshareFromBytes(kg1Result.Keyshare)
	if err != nil {
		t.Fatalf("failed to load old keyshare: %v", err)
	}
	defer session.DklsKeyshareFree(oldKeyshareHandle)

	qcSetup, err := session.DklsQcSetupMsgNew(
		oldKeyshareHandle,
		threshold,
		newParticipants,
		[]int{0, 1, 2},    // old parties: party1, party2, party3 (indices in newParticipants)
		[]int{0, 1, 2, 3}, // new parties: all parties in new quorum (party1, party2, party3, party4)
	)
	if err != nil {
		t.Fatalf("failed to create QC setup: %v", err)
	}

	// Create QC sessions
	qcParty1, err := NewQuorumChangeSession(qcSetup, "qc", "party1", newParticipants, threshold, kg1Result.Keyshare)
	if err != nil {
		t.Fatalf("failed to create QC session party1: %v", err)
	}

	qcParty2, err := NewQuorumChangeSession(qcSetup, "qc", "party2", newParticipants, threshold, kg2Result.Keyshare)
	if err != nil {
		t.Fatalf("failed to create QC session party2: %v", err)
	}

	qcParty3, err := NewQuorumChangeSession(qcSetup, "qc", "party3", newParticipants, threshold, kg3Result.Keyshare)
	if err != nil {
		t.Fatalf("failed to create QC session party3: %v", err)
	}

	qcParty4, err := NewQuorumChangeSession(qcSetup, "qc", "party4", newParticipants, threshold, nil) // new party
	if err != nil {
		t.Fatalf("failed to create QC session party4: %v", err)
	}

	// Run QC to completion
	qcSessions := map[string]Session{
		"party1": qcParty1,
		"party2": qcParty2,
		"party3": qcParty3,
		"party4": qcParty4,
	}
	qcResults := runToCompletion(t, qcSessions)

	// Verify all results
	qcParty1Result := qcResults["party1"]
	qcParty2Result := qcResults["party2"]
	qcParty3Result := qcResults["party3"]
	qcParty4Result := qcResults["party4"]

	if len(qcParty1Result.Keyshare) == 0 {
		t.Error("QC party1 keyshare is empty")
	}
	if len(qcParty2Result.Keyshare) == 0 {
		t.Error("QC party2 keyshare is empty")
	}
	if len(qcParty3Result.Keyshare) == 0 {
		t.Error("QC party3 keyshare is empty")
	}
	if len(qcParty4Result.Keyshare) == 0 {
		t.Error("QC party4 keyshare is empty")
	}
	if qcParty1Result.Signature != nil {
		t.Error("QC should not return signature")
	}
	if len(qcParty1Result.Participants) != 4 {
		t.Errorf("expected 4 participants, got %d", len(qcParty1Result.Participants))
	}

	// Verify keyID changes after QC
	if qcParty1Result.KeyID == originalKeyID {
		t.Error("keyID should change after QC")
	}

	// Verify public key remains the same
	if len(qcParty1Result.PublicKey) != len(originalPublicKey) {
		t.Errorf("public key length changed: got %d, want %d", len(qcParty1Result.PublicKey), len(originalPublicKey))
	}
	for i := range originalPublicKey {
		if qcParty1Result.PublicKey[i] != originalPublicKey[i] {
			t.Errorf("public key changed at index %d", i)
			break
		}
	}

	// Verify keyshare is different
	keyshareChanged := false
	if len(qcParty1Result.Keyshare) != len(kg1Result.Keyshare) {
		keyshareChanged = true
	} else {
		for i := range kg1Result.Keyshare {
			if qcParty1Result.Keyshare[i] != kg1Result.Keyshare[i] {
				keyshareChanged = true
				break
			}
		}
	}
	if !keyshareChanged {
		t.Error("keyshare did not change after QC")
	}
}
