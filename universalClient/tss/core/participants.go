package core

import (
	"fmt"
	"sort"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

type partySet struct {
	list    []*tss.UniversalValidator
	idx     map[string]*tss.UniversalValidator
	encoded []byte
}

func newPartySet(participants []*tss.UniversalValidator) (*partySet, error) {
	if len(participants) == 0 {
		return nil, errMissingParticipants
	}
	copied := make([]*tss.UniversalValidator, len(participants))
	copy(copied, participants)

	sort.Slice(copied, func(i, j int) bool {
		return copied[i].PartyID() < copied[j].PartyID()
	})

	idx := make(map[string]*tss.UniversalValidator, len(copied))
	for _, p := range copied {
		if p == nil {
			return nil, fmt.Errorf("nil participant")
		}
		partyID := p.PartyID()
		if partyID == "" || p.PeerID() == "" || len(p.Multiaddrs()) == 0 {
			return nil, fmt.Errorf("participant %s missing peer or multiaddr", partyID)
		}
		if _, exists := idx[partyID]; exists {
			return nil, fmt.Errorf("duplicate participant %s", partyID)
		}
		idx[partyID] = p
	}

	return &partySet{
		list: copied,
		idx:  idx,
	}, nil
}

func (p *partySet) len() int { return len(p.list) }

func (p *partySet) contains(partyID string) bool {
	_, ok := p.idx[partyID]
	return ok
}

func (p *partySet) peerInfo(partyID string) (*tss.UniversalValidator, bool) {
	participant, ok := p.idx[partyID]
	return participant, ok
}

func (p *partySet) encodedIDs() []byte {
	if p.encoded != nil {
		return p.encoded
	}
	ids := make([]byte, 0, len(p.list)*10)
	for i, party := range p.list {
		if i > 0 {
			ids = append(ids, 0)
		}
		ids = append(ids, []byte(party.PartyID())...)
	}
	p.encoded = ids
	return ids
}
