// Package coordinator provides participant management for TSS operations.
package coordinator

import (
	"context"
	"fmt"
	"sort"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

// StaticParticipantProvider provides a static list of participants (useful for testing).
type StaticParticipantProvider struct {
	participants map[string][]*tss.UniversalValidator // protocolType -> participants
	logger       zerolog.Logger
}

// NewStaticParticipantProvider creates a static participant provider.
func NewStaticParticipantProvider(logger zerolog.Logger) *StaticParticipantProvider {
	return &StaticParticipantProvider{
		participants: make(map[string][]*tss.UniversalValidator),
		logger:       logger,
	}
}

// SetParticipants sets the participants for a protocol type.
func (p *StaticParticipantProvider) SetParticipants(protocolType string, participants []*tss.UniversalValidator) {
	// Sort by PartyID for consistency
	sorted := make([]*tss.UniversalValidator, len(participants))
	copy(sorted, participants)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].PartyID() < sorted[j].PartyID()
	})
	p.participants[protocolType] = sorted
}

// GetParticipants implements ParticipantProvider.
func (p *StaticParticipantProvider) GetParticipants(ctx context.Context, protocolType string, blockNumber uint64) ([]*tss.UniversalValidator, error) {
	participants, ok := p.participants[protocolType]
	if !ok {
		// Fallback to default participants if protocol-specific not set
		participants, ok = p.participants[""]
		if !ok {
			return nil, fmt.Errorf("no participants configured for protocol type %s", protocolType)
		}
	}

	// Return a copy to prevent modification
	result := make([]*tss.UniversalValidator, len(participants))
	copy(result, participants)
	return result, nil
}
