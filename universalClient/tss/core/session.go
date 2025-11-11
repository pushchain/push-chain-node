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

	blockNumber        uint64
	coordinatorPartyID string
	coordinatorPeerID  string
	participants       map[string]string
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

func (s *sessionState) setMetadata(blockNumber uint64, coordinatorPartyID, coordinatorPeerID string, parties *partySet) {
	s.blockNumber = blockNumber
	s.coordinatorPartyID = coordinatorPartyID
	s.coordinatorPeerID = coordinatorPeerID

	if parties == nil {
		return
	}

	if len(parties.list) > 0 {
		s.participants = make(map[string]string, len(parties.list))
		for _, party := range parties.list {
			s.participants[party.PeerID] = party.PartyID
		}
	}
}

func (s *sessionState) isKnownPeer(peerID string) bool {
	if len(s.participants) == 0 {
		return false
	}
	_, ok := s.participants[peerID]
	return ok
}

func (s *sessionState) isCoordinatorPeer(peerID string) bool {
	if s.coordinatorPeerID == "" {
		return false
	}
	return s.coordinatorPeerID == peerID
}
