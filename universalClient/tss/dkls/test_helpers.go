package dkls

import (
	"testing"
)

// runToCompletion runs a protocol to completion for all provided sessions.
// sessions: map of partyID -> Session
// Returns: map of partyID -> Result
func runToCompletion(t *testing.T, sessions map[string]Session) map[string]*Result {
	t.Helper()

	done := make(map[string]bool)
	results := make(map[string]*Result)

	// Initialize done map
	for partyID := range sessions {
		done[partyID] = false
	}

	// Run until all parties are done
	allDone := false
	for !allDone {
		allDone = true

		// Step each party that's not done
		for partyID, session := range sessions {
			if done[partyID] {
				continue
			}

			msgs, sessionDone, err := session.Step()
			if err != nil {
				t.Fatalf("%s Step() error: %v", partyID, err)
			}

			// Route messages to recipients
			for _, msg := range msgs {
				recipientSession, exists := sessions[msg.Receiver]
				if !exists {
					t.Fatalf("message recipient %s not found in sessions", msg.Receiver)
				}
				if err := recipientSession.InputMessage(msg.Data); err != nil {
					t.Fatalf("failed to input message to %s: %v", msg.Receiver, err)
				}
			}

			if sessionDone {
				done[partyID] = true
				result, err := session.GetResult()
				if err != nil {
					t.Fatalf("%s GetResult() failed: %v", partyID, err)
				}
				results[partyID] = result
			} else {
				allDone = false
			}
		}
	}

	// Verify all parties got results
	for partyID := range sessions {
		if _, exists := results[partyID]; !exists {
			t.Fatalf("%s did not complete", partyID)
		}
	}

	// Close all sessions
	for partyID, session := range sessions {
		session.Close()
		_ = partyID // avoid unused variable if needed
	}

	return results
}
