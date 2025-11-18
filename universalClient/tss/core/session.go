package core

import (
	"fmt"
	"time"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

type sessionState struct {
	protocol        tss.ProtocolType
	eventID         string
	setupCh         chan *setupEnvelope
	payloadCh       chan []byte
	setupDeadline   time.Duration
	payloadDeadline time.Duration

	coordinatorPeerID string
	knownPeers        map[string]bool // peerID -> true
}

func newSessionState(protocol tss.ProtocolType, eventID string, setupTimeout, payloadTimeout time.Duration) *sessionState {
	return &sessionState{
		protocol:        protocol,
		eventID:         eventID,
		setupCh:         make(chan *setupEnvelope, 1),
		payloadCh:       make(chan []byte, 256),
		setupDeadline:   setupTimeout,
		payloadDeadline: payloadTimeout,
	}
}

func (s *sessionState) enqueueSetup(env *setupEnvelope) error {
	select {
	case s.setupCh <- env:
		return nil
	default:
		return fmt.Errorf("setup already received for %s", s.eventID)
	}
}

func (s *sessionState) enqueuePayload(data []byte) error {
	buf := make([]byte, len(data))
	copy(buf, data)
	select {
	case s.payloadCh <- buf:
		return nil
	default:
		return fmt.Errorf("payload buffer full for %s", s.eventID)
	}
}

func (s *sessionState) setMetadata(coordinatorPeerID string, parties *partySet) {
	s.coordinatorPeerID = coordinatorPeerID

	if parties == nil {
		return
	}

	if len(parties.list) > 0 {
		s.knownPeers = make(map[string]bool, len(parties.list))
		for _, party := range parties.list {
			s.knownPeers[party.PeerID()] = true
		}
	}
}

func (s *sessionState) isParticipant(peerID string) bool {
	if s.knownPeers == nil {
		return false
	}
	return s.knownPeers[peerID]
}

func (s *sessionState) isCoordinator(peerID string) bool {
	if s.coordinatorPeerID == "" {
		return false
	}
	return s.coordinatorPeerID == peerID
}
