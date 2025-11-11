package mock

import (
	"context"
	"fmt"
	"sync"

	"github.com/pushchain/push-chain-node/universalClient/tss/transport"
)

// Transport is a simple in-memory implementation used by tests and local demos.
type Transport struct {
	id        string
	handler   transport.Handler
	handlerMu sync.RWMutex

	peersMu sync.RWMutex
	peers   map[string]*Transport
}

// New creates a mock transport with the given ID.
func New(id string) *Transport {
	return &Transport{
		id:    id,
		peers: make(map[string]*Transport),
	}
}

// Link connects two mock transports so they can exchange messages.
func Link(a, b *Transport) {
	a.peersMu.Lock()
	a.peers[b.id] = b
	a.peersMu.Unlock()

	b.peersMu.Lock()
	b.peers[a.id] = a
	b.peersMu.Unlock()
}

func (t *Transport) ID() string { return t.id }

func (t *Transport) ListenAddrs() []string { return []string{"mock://" + t.id} }

func (t *Transport) RegisterHandler(handler transport.Handler) error {
	t.handlerMu.Lock()
	defer t.handlerMu.Unlock()
	if t.handler != nil {
		return fmt.Errorf("mock transport: handler already registered")
	}
	t.handler = handler
	return nil
}

func (t *Transport) EnsurePeer(peerID string, _ []string) error {
	t.peersMu.Lock()
	defer t.peersMu.Unlock()
	if _, ok := t.peers[peerID]; !ok {
		return fmt.Errorf("mock transport: unknown peer %s", peerID)
	}
	return nil
}

func (t *Transport) Send(ctx context.Context, peerID string, payload []byte) error {
	t.peersMu.RLock()
	target, ok := t.peers[peerID]
	t.peersMu.RUnlock()
	if !ok {
		return fmt.Errorf("mock transport: peer %s not linked", peerID)
	}

	target.handlerMu.RLock()
	handler := target.handler
	target.handlerMu.RUnlock()
	if handler == nil {
		return fmt.Errorf("mock transport: peer %s missing handler", peerID)
	}

	go handler(ctx, t.id, payload)
	return nil
}

func (t *Transport) Close() error {
	return nil
}
