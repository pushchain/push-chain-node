package dkls

// Message represents a protocol message that needs to be sent to a participant.
type Message struct {
	Receiver string // Party ID of the receiver
	Data     []byte // Protocol message data
}

// Session manages a DKLS protocol session (keygen, keyrefresh, or sign).
type Session interface {
	// Step processes the next protocol step and returns messages to send.
	// Returns (messages, finished, error)
	Step() ([]Message, bool, error)

	// InputMessage processes an incoming protocol message.
	InputMessage(data []byte) error

	// GetResult returns the result when finished.
	// For keygen/keyrefresh: returns keyshare (signature will be nil)
	// For sign: returns signature (keyshare will be nil)
	GetResult() (*Result, error)

	// Close cleans up the session.
	Close()
}

// Result contains the result of a DKLS protocol operation.
type Result struct {
	Keyshare     []byte   // For keygen/keyrefresh
	Signature    []byte   // For sign
	Participants []string // All participants
}
