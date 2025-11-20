package node

import (
	"context"

	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/pushchain/push-chain-node/universalClient/tss/core"
)

// eventStoreAdapter implements core.EventStore using the node's database and data provider.
type eventStoreAdapter struct {
	node *Node
}

// GetEvent retrieves event information for session recovery.
func (e *eventStoreAdapter) GetEvent(eventID string) (*core.EventInfo, error) {
	event, err := e.node.eventStore.GetEvent(eventID)
	if err != nil {
		return nil, err
	}

	allValidators, err := e.node.dataProvider.GetUniversalValidators(context.Background())
	if err != nil {
		return nil, err
	}

	var participants []*tss.UniversalValidator
	for _, v := range allValidators {
		if v.Status == tss.UVStatusActive {
			participants = append(participants, v)
		}
	}

	return &core.EventInfo{
		EventID:      event.EventID,
		BlockNumber:  event.BlockNumber,
		ProtocolType: event.ProtocolType,
		Status:       event.Status,
		Participants: participants,
	}, nil
}
