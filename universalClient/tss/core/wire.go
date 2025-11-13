package core

import (
	"encoding/json"
	"fmt"

	"github.com/pushchain/push-chain-node/universalClient/tss"
)

type messageType string

const (
	messageSetup   messageType = "setup"
	messagePayload messageType = "payload"
)

type wireMessage struct {
	Protocol tss.ProtocolType `json:"protocol"`
	Type     messageType      `json:"type"`
	EventID  string           `json:"event_id"`
	Sender   string           `json:"sender"`
	Receiver string           `json:"receiver,omitempty"`
	Setup    *setupEnvelope   `json:"setup,omitempty"`
	Payload  []byte           `json:"payload,omitempty"`
}

type setupEnvelope struct {
	KeyID        string             `json:"key_id"`
	Threshold    int                `json:"threshold"`
	Participants []participantEntry `json:"participants"`
	Data         []byte             `json:"data"`
}

type participantEntry struct {
	PartyID string `json:"party_id"`
	PeerID  string `json:"peer_id"`
}

func encodeWire(msg *wireMessage) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("nil wire message")
	}
	return json.Marshal(msg)
}

func decodeWire(data []byte) (*wireMessage, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty payload")
	}
	var msg wireMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if msg.Protocol == "" || msg.Type == "" || msg.EventID == "" {
		return nil, fmt.Errorf("invalid wire message")
	}
	return &msg, nil
}
