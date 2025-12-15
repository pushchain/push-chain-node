package networking

// Message represents a message to be sent to a peer.
// This is a simple wrapper that can be used by higher-level protocols.
type Message struct {
	Receiver string // Peer ID of the receiver
	Data     []byte // Raw message data
}

// EncodeMessage encodes a message into bytes.
// This is a simple pass-through for now, but can be extended with framing, compression, etc.
func EncodeMessage(msg Message) []byte {
	return msg.Data
}

// DecodeMessage decodes bytes into a message.
// This is a simple pass-through for now, but can be extended with framing, compression, etc.
func DecodeMessage(data []byte) Message {
	return Message{
		Data: data,
	}
}
