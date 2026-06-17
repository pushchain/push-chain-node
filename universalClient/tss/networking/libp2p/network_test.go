package libp2p

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

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

