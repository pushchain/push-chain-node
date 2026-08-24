package libp2p

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReadFramed_RoundTrip(t *testing.T) {
	payload := []byte("hello tss")
	var buf bytes.Buffer
	require.NoError(t, writeFramed(&buf, payload))

	got, err := readFramed(&buf)
	require.NoError(t, err)
	assert.Equal(t, payload, got)
}

func TestReadFramed_RejectsOversizeLengthPrefix(t *testing.T) {
	// Craft a frame whose length prefix claims more than MaxFrameSize.
	// readFramed must reject before allocating MaxFrameSize+1 bytes.
	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.BigEndian, uint32(MaxFrameSize+1)))
	// No payload bytes follow — readFramed should fail on the length check
	// before attempting to read the (non-existent) body.

	_, err := readFramed(&buf)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
}

func TestReadFramed_AcceptsAtMaxFrameSize(t *testing.T) {
	// Boundary: a frame of exactly MaxFrameSize bytes must be accepted.
	// We don't actually allocate 16 MiB in the test buffer; instead we
	// validate the length-check path with a reader that returns EOF after
	// the length prefix and assert the failure mode is the read error,
	// not the size-cap error.
	var buf bytes.Buffer
	require.NoError(t, binary.Write(&buf, binary.BigEndian, uint32(MaxFrameSize)))

	_, err := readFramed(&buf)
	require.Error(t, err)
	// Should be EOF/UnexpectedEOF on the body read, NOT the size-cap rejection.
	assert.NotContains(t, err.Error(), "exceeds maximum")
	assert.True(t, err == io.EOF || err == io.ErrUnexpectedEOF, "expected EOF on truncated body, got: %v", err)
}

func TestWriteFramed_RejectsOversizePayload(t *testing.T) {
	// writeFramed must symmetric-cap so a misbehaving local sender cannot
	// produce a frame that the receiving peer would itself reject. Avoids
	// silent protocol drops where the wire format crosses the line.
	oversize := make([]byte, MaxFrameSize+1)
	var buf bytes.Buffer
	err := writeFramed(&buf, oversize)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds maximum")
	// Buffer must not contain partial data — the check happens before any write.
	assert.Equal(t, 0, buf.Len(), "writeFramed must not emit any bytes when rejecting")
}

func TestWriteFramed_AcceptsAtMaxFrameSize(t *testing.T) {
	// Boundary: payload of exactly MaxFrameSize must round-trip.
	payload := make([]byte, MaxFrameSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}
	var buf bytes.Buffer
	require.NoError(t, writeFramed(&buf, payload))

	got, err := readFramed(&buf)
	require.NoError(t, err)
	assert.Equal(t, len(payload), len(got))
	assert.Equal(t, payload[0], got[0])
	assert.Equal(t, payload[len(payload)-1], got[len(got)-1])
}

// allowlist is a mutable peer-ID allowlist used as a test Authorizer.
type allowlist struct {
	mu    sync.RWMutex
	peers map[string]bool
}

func newAllowlist() *allowlist {
	return &allowlist{peers: make(map[string]bool)}
}

func (a *allowlist) allow(peerID string) {
	a.mu.Lock()
	a.peers[peerID] = true
	a.mu.Unlock()
}

func (a *allowlist) revoke(peerID string) {
	a.mu.Lock()
	delete(a.peers, peerID)
	a.mu.Unlock()
}

func (a *allowlist) authorized(peerID string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.peers[peerID]
}

func newTestNetwork(t *testing.T, authorizer func(string) bool) *Network {
	t.Helper()
	n, err := New(context.Background(), Config{
		ListenAddrs: []string{"/ip4/127.0.0.1/tcp/0"},
		DialTimeout: 5 * time.Second,
		IOTimeout:   5 * time.Second,
		Authorizer:  authorizer,
	}, zerolog.New(io.Discard))
	require.NoError(t, err)
	t.Cleanup(func() { _ = n.Close() })
	return n
}

func connectPeer(t *testing.T, from *Network, to *Network) {
	t.Helper()
	require.NoError(t, from.EnsurePeer(to.ID(), to.ListenAddrs()))
}

func collectMessages(t *testing.T, n *Network) <-chan string {
	t.Helper()
	msgs := make(chan string, 64)
	require.NoError(t, n.RegisterHandler(func(peerID string, data []byte) {
		msgs <- peerID + ":" + string(data)
	}))
	return msgs
}

func TestNetwork_RejectsUnknownPeer(t *testing.T) {
	acl := newAllowlist()
	receiver := newTestNetwork(t, acl.authorized)
	rogue := newTestNetwork(t, nil)
	msgs := collectMessages(t, receiver)

	connectPeer(t, rogue, receiver)
	err := rogue.Send(context.Background(), receiver.ID(), []byte("intrusion"))
	require.Error(t, err, "unauthenticated peer must not reach the TSS protocol")

	select {
	case m := <-msgs:
		t.Fatalf("handler received message from unauthorized peer: %s", m)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNetwork_AuthorizedPeerDeliversDuringUnauthenticatedFlood(t *testing.T) {
	acl := newAllowlist()
	receiver := newTestNetwork(t, acl.authorized)
	validator := newTestNetwork(t, nil)
	acl.allow(validator.ID())
	msgs := collectMessages(t, receiver)

	const rogues = 8
	var wg sync.WaitGroup
	for i := range rogues {
		rogue := newTestNetwork(t, nil)
		connectPeer(t, rogue, receiver)
		wg.Add(1)
		go func(r *Network, i int) {
			defer wg.Done()
			for j := range 5 {
				_ = r.Send(context.Background(), receiver.ID(), fmt.Appendf(nil, "flood-%d-%d", i, j))
			}
		}(rogue, i)
	}

	connectPeer(t, validator, receiver)
	require.NoError(t, validator.Send(context.Background(), receiver.ID(), []byte("ack")))
	wg.Wait()

	select {
	case m := <-msgs:
		assert.Equal(t, validator.ID()+":ack", m)
	case <-time.After(5 * time.Second):
		t.Fatal("validator message not delivered during unauthenticated flood")
	}

	select {
	case m := <-msgs:
		t.Fatalf("received unexpected message: %s", m)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNetwork_ResetsStreamAfterPeerRevoked(t *testing.T) {
	acl := newAllowlist()
	receiver := newTestNetwork(t, acl.authorized)
	validator := newTestNetwork(t, nil)
	acl.allow(validator.ID())
	msgs := collectMessages(t, receiver)

	connectPeer(t, validator, receiver)
	require.NoError(t, validator.Send(context.Background(), receiver.ID(), []byte("before")))
	select {
	case m := <-msgs:
		assert.Equal(t, validator.ID()+":before", m)
	case <-time.After(5 * time.Second):
		t.Fatal("message from authorized peer not delivered")
	}

	// Revoke: the existing connection survives the gater, but handleStream
	// must reset new streams from the now-unauthorized peer.
	acl.revoke(validator.ID())
	_ = validator.Send(context.Background(), receiver.ID(), []byte("after"))

	select {
	case m := <-msgs:
		t.Fatalf("handler received message from revoked peer: %s", m)
	case <-time.After(500 * time.Millisecond):
	}
}
