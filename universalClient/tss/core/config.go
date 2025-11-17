package core

import (
	"errors"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/keyshare"
	"github.com/pushchain/push-chain-node/universalClient/tss/transport"
)

// Config controls how the TSS service operates on this node.
type Config struct {
	PartyID        string
	SetupTimeout   time.Duration
	MessageTimeout time.Duration
	Logger         zerolog.Logger
}

func (c *Config) setDefaults() {
	if c.SetupTimeout == 0 {
		c.SetupTimeout = 30 * time.Second
	}
	if c.MessageTimeout == 0 {
		c.MessageTimeout = 30 * time.Second
	}
}

// EventStore provides access to TSS events for session recovery.
type EventStore interface {
	GetEvent(eventID string) (*EventInfo, error)
}

// EventInfo contains information about a TSS event.
type EventInfo struct {
	EventID      string
	BlockNumber  uint64
	ProtocolType string
	Status       string
	Participants []*tss.UniversalValidator
}

// Dependencies groups the runtime dependencies required by the service.
type Dependencies struct {
	Transport     transport.Transport
	KeyshareStore *keyshare.Manager
	EventStore    EventStore // Optional: for session recovery
}

// KeygenRequest triggers a DKLS key generation flow.
type KeygenRequest struct {
	EventID      string
	KeyID        string
	Threshold    int
	BlockNumber  uint64
	Participants []*tss.UniversalValidator
}

type KeyrefreshRequest struct {
	EventID      string
	KeyID        string
	Threshold    int
	BlockNumber  uint64
	Participants []*tss.UniversalValidator
}

type SignRequest struct {
	EventID      string
	KeyID        string
	Threshold    int
	MessageHash  []byte
	ChainPath    []byte
	BlockNumber  uint64
	Participants []*tss.UniversalValidator
}

type KeygenResult struct {
	KeyID      string
	PublicKey  []byte
	NumParties int
}

type KeyrefreshResult struct {
	KeyID      string
	PublicKey  []byte
	NumParties int
}

type SignResult struct {
	KeyID      string
	Signature  []byte
	NumParties int
}

var (
	errInvalidConfig       = errors.New("tss: invalid config")
	errMissingParticipants = errors.New("tss: participants missing")
	errMissingThreshold    = errors.New("tss: invalid threshold")
	errLocalNotIncluded    = errors.New("tss: local party not included")
	errKeyExists           = errors.New("tss: key already exists")
	errKeyMissing          = errors.New("tss: keyshare missing")
	errUnknownSession      = errors.New("tss: unknown session")
	errSetupTimeout        = errors.New("tss: setup timed out")
	errPayloadTimeout      = errors.New("tss: payload timed out")
)
