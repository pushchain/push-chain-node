package core

import (
	"fmt"
	"sort"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

type partySet struct {
	list    []tss.Participant
	idx     map[string]tss.Participant
	encoded []byte
}

func newPartySet(participants []tss.Participant) (*partySet, error) {
	if len(participants) == 0 {
		return nil, errMissingParticipants
	}
	copied := make([]tss.Participant, len(participants))
	copy(copied, participants)

	sort.Slice(copied, func(i, j int) bool {
		return copied[i].PartyID < copied[j].PartyID
	})

	idx := make(map[string]tss.Participant, len(copied))
	for _, p := range copied {
		if p.PartyID == "" || p.PeerID == "" || len(p.Multiaddrs) == 0 {
			return nil, fmt.Errorf("participant %s missing peer or multiaddr", p.PartyID)
		}
		if _, exists := idx[p.PartyID]; exists {
			return nil, fmt.Errorf("duplicate participant %s", p.PartyID)
		}
		idx[p.PartyID] = p
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

func (p *partySet) peerInfo(partyID string) (tss.Participant, bool) {
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
		ids = append(ids, []byte(party.PartyID)...)
	}
	p.encoded = ids
	return ids
}
