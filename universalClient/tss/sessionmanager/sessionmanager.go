package sessionmanager

import (
	"context"

	"github.com/rs/zerolog"
)

// SessionManager manages TSS protocol sessions and handles incoming messages.
type SessionManager struct {
	logger zerolog.Logger
}

// NewSessionManager creates a new session manager.
func NewSessionManager(logger zerolog.Logger) *SessionManager {
	return &SessionManager{
		logger: logger,
	}
}

// HandleIncomingMessage handles an incoming message.
// peerID: The peer ID of the sender
// data: The raw message bytes
func (sm *SessionManager) HandleIncomingMessage(ctx context.Context, peerID string, data []byte) error {
	sm.logger.Debug().
		Str("peer_id", peerID).
		Int("data_len", len(data)).
		Msg("handling incoming message")

	// TODO: Unmarshal and implement message handling logic

	return nil
}
