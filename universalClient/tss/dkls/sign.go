package dkls

import (
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto/secp256k1"
	"github.com/pkg/errors"

	session "go-wrapper/go-dkls/sessions"
)

// signSession implements Session.
type signSession struct {
	sessionID    string
	partyID      string
	handle       session.Handle
	payloadCh    chan []byte
	participants []string
	keyID        string // Store keyID for GetResult (can't extract from sign session handle)
	publicKey    []byte // Store publicKey for GetResult (can't extract from sign session handle)
	messageHash  []byte // Store messageHash for signature verification
}

// NewSignSession creates a new sign session.
// setupData: The setup message (must be provided - keyID is extracted from keyshare by caller)
// sessionID: Session identifier (typically eventID)
// partyID: This node's party ID
// participants: List of participant party IDs (sorted)
// keyshare: The keyshare to use for signing (keyID is extracted from keyshare)
// messageHash: The message hash to sign
// chainPath: Optional chain path (nil if empty)
func NewSignSession(
	setupData []byte,
	sessionID string,
	partyID string,
	participants []string,
	keyshare []byte,
	messageHash []byte,
	chainPath []byte,
) (Session, error) {
	// setupData is required - check first
	if len(setupData) == 0 {
		return nil, fmt.Errorf("setupData is required")
	}
	if partyID == "" {
		return nil, fmt.Errorf("party ID required")
	}
	if len(participants) == 0 {
		return nil, fmt.Errorf("participants required")
	}
	if len(keyshare) == 0 {
		return nil, fmt.Errorf("keyshare required")
	}
	if len(messageHash) == 0 {
		return nil, fmt.Errorf("message hash required")
	}

	// Load keyshare
	keyshareHandle, err := session.DklsKeyshareFromBytes(keyshare)
	if err != nil {
		return nil, fmt.Errorf("failed to load keyshare: %w", err)
	}

	// Extract keyID and publicKey from keyshare before creating sign session
	// (we can't extract them from sign session handle later)
	keyIDBytes, err := session.DklsKeyshareKeyID(keyshareHandle)
	if err != nil {
		session.DklsKeyshareFree(keyshareHandle)
		return nil, fmt.Errorf("failed to extract keyID from keyshare: %w", err)
	}
	// Sanitize keyID by hex-encoding it to ensure it's safe for use as a filename
	keyID := hex.EncodeToString(keyIDBytes)

	publicKey, err := session.DklsKeysharePublicKey(keyshareHandle)
	if err != nil {
		session.DklsKeyshareFree(keyshareHandle)
		return nil, fmt.Errorf("failed to extract publicKey from keyshare: %w", err)
	}

	// Create session from setup
	handle, err := session.DklsSignSessionFromSetup(setupData, []byte(partyID), keyshareHandle)
	if err != nil {
		session.DklsKeyshareFree(keyshareHandle)
		return nil, fmt.Errorf("failed to create sign session: %w", err)
	}
	// Free keyshare handle after creating sign session (sign session has its own copy)
	session.DklsKeyshareFree(keyshareHandle)

	return &signSession{
		sessionID:    sessionID,
		partyID:      partyID,
		handle:       handle,
		payloadCh:    make(chan []byte, 256),
		participants: participants,
		keyID:        keyID,
		publicKey:    publicKey,
		messageHash:  messageHash, // Store messageHash for verification
	}, nil
}

// Step processes the next protocol step and returns messages to send.
func (s *signSession) Step() ([]Message, bool, error) {
	// Process any queued input messages first
	select {
	case payload := <-s.payloadCh:
		finished, err := session.DklsSignSessionInputMessage(s.handle, payload)
		if err != nil {
			return nil, false, fmt.Errorf("failed to process input message: %w", err)
		}
		if finished {
			return nil, true, nil
		}
	default:
	}

	// Get output messages
	var messages []Message
	for {
		msgData, err := session.DklsSignSessionOutputMessage(s.handle)
		if err != nil {
			return nil, false, fmt.Errorf("failed to get output message: %w", err)
		}
		if len(msgData) == 0 {
			break
		}

		for idx := 0; idx < len(s.participants); idx++ {
			receiverBytes, err := session.DklsSignSessionMessageReceiver(s.handle, msgData, idx)
			if err != nil {
				return nil, false, fmt.Errorf("failed to get message receiver: %w", err)
			}
			receiver := string(receiverBytes)
			if receiver == "" {
				break
			}

			if receiver == s.partyID {
				if err := s.InputMessage(msgData); err != nil {
					return nil, false, fmt.Errorf("failed to queue local message: %w", err)
				}
				continue
			}

			messages = append(messages, Message{
				Receiver: receiver,
				Data:     msgData,
			})
		}
	}

	return messages, false, nil
}

// InputMessage processes an incoming protocol message.
func (s *signSession) InputMessage(data []byte) error {
	buf := make([]byte, len(data))
	copy(buf, data)
	select {
	case s.payloadCh <- buf:
		return nil
	default:
		return fmt.Errorf("payload buffer full for session %s", s.sessionID)
	}
}

// GetResult returns the result when finished.
func (s *signSession) GetResult() (*Result, error) {
	sig, err := session.DklsSignSessionFinish(s.handle)
	if err != nil {
		return nil, fmt.Errorf("failed to finish sign session: %w", err)
	}

	// Verify signature before returning
	verified, verifyErr := s.verifySignature(s.publicKey, sig, s.messageHash)
	if verifyErr != nil {
		return nil, fmt.Errorf("signature verification error: %w", verifyErr)
	}
	if !verified {
		return nil, errors.New("signature verification failed")
	}

	// Return participants list (copy to avoid mutation)
	participants := make([]string, len(s.participants))
	copy(participants, s.participants)

	return &Result{
		Keyshare:     nil,
		Signature:    sig,
		KeyID:        s.keyID,     // Use stored keyID
		PublicKey:    s.publicKey, // Use stored publicKey
		Participants: participants,
	}, nil
}

// verifySignature verifies an ECDSA signature using secp256k1.
// publicKey: Compressed public key (33 bytes)
// signature: ECDSA signature (64 or 65 bytes: r || s [|| recovery_id])
// messageHash: SHA256 hash of the message (32 bytes)
func (s *signSession) verifySignature(publicKey, signature, messageHash []byte) (bool, error) {
	if len(publicKey) != 33 {
		return false, errors.Errorf("public key must be 33 bytes (compressed), got %d bytes", len(publicKey))
	}
	if len(signature) != 64 && len(signature) != 65 {
		return false, errors.Errorf("signature must be 64 or 65 bytes (r || s [|| recovery_id]), got %d bytes", len(signature))
	}
	if len(messageHash) != 32 {
		return false, errors.Errorf("message hash must be 32 bytes, got %d bytes", len(messageHash))
	}

	// Use only first 64 bytes (r || s), ignore recovery ID if present
	sigBytes := signature
	if len(signature) == 65 {
		sigBytes = signature[:64]
	}

	// Decompress public key
	vkX, vkY := secp256k1.DecompressPubkey(publicKey)
	if vkX == nil || vkY == nil {
		return false, errors.New("failed to decompress public key")
	}

	// Create ECDSA public key
	vk := ecdsa.PublicKey{
		Curve: secp256k1.S256(),
		X:     vkX,
		Y:     vkY,
	}

	// Extract r and s from signature (first 32 bytes for r, next 32 bytes for s)
	r := big.NewInt(0).SetBytes(sigBytes[:32])
	sigS := big.NewInt(0).SetBytes(sigBytes[32:64])

	// Verify signature
	verified := ecdsa.Verify(&vk, messageHash, r, sigS)

	return verified, nil
}

// Close cleans up the session.
func (s *signSession) Close() {
	if s.handle != 0 {
		session.DklsSignSessionFree(s.handle)
		s.handle = 0
	}
}
