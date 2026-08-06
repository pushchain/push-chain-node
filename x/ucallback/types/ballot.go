package types

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"cosmossdk.io/collections"
)

// ReadBallotDomain separates read-result ballot keys from every other ballot
// namespace on the chain, so a digest collision across modules is not possible.
var ReadBallotDomain = collections.NewPrefix(1)

// VotesThresholdNumerator / VotesThresholdDenominator give the >2/3 quorum used
// chain-wide. Mirrors x/uexecutor's constants so read ballots finalize on the same
// threshold as inbound and outbound ones.
const (
	VotesThresholdNumerator   = 2
	VotesThresholdDenominator = 3
)

// DefaultExpiryAfterBlocks is the ballot-level expiry passed to VoteOnBallot.
//
// Set high enough to be inert (~19 years at 6s blocks), matching x/uexecutor. A
// read request already has its own deadline — ReadRequest.ExpiryBlockHeight, set
// by the app and enforced by the contract. Giving the ballot a second, shorter
// clock would let it die while its request is still live, stranding a record that
// can neither fulfil nor expire until the real deadline arrives.
const DefaultExpiryAfterBlocks = 100_000_000

// GetReadBallotKey derives the ballot a (requestId, observation) pair votes on.
//
// The ballot model is binary: validators vote SUCCESS or FAILURE on a key that
// already encodes the observation. Agreement is therefore expressed by arriving at
// the same key — two validators reporting different result data produce different
// ballots, and neither reaches quorum until enough validators agree.
//
// Two consequences follow, and both are load-bearing:
//
//  1. Every field that callers must agree on has to be in this digest. Omitting one
//     would let validators finalize a ballot while disagreeing about it.
//
//  2. Nothing validator-local may be in it. This is why ReadResult carries no error
//     message: free-text error strings differ per validator, so including one would
//     scatter honest validators across distinct ballots and quorum would never form.
//     x/uexecutor's outbound key does hash an ErrorMsg (keys.go:158) — we deliberately
//     do not follow it there.
//
// Aggregates are excluded, see readResultFields.
func GetReadBallotKey(requestID string, result *ReadResult) (string, error) {
	if requestID == "" {
		return "", fmt.Errorf("cannot derive ballot key: empty request id")
	}
	if result == nil {
		return "", fmt.Errorf("cannot derive ballot key: nil result")
	}

	parts := append([]string{strings.ToLower(requestID)}, readResultFields(result)...)
	return hashFields(ReadBallotDomain, parts...), nil
}

// readResultFields renders the consensus-relevant part of an observation.
//
// `aggregates` is deliberately absent. It is reserved for v2 MEDIAN mode, where
// validators submit differing per-field values that are reduced afterwards — the
// opposite of the identical-observation model this key assumes. Hashing it now
// would be harmless (it is always empty in v1) but would silently become
// consensus-breaking the moment v2 populates it: the same read would map to a
// different ballot before and after the upgrade. Excluding it from the start keeps
// v2 a purely additive change.
func readResultFields(r *ReadResult) []string {
	return []string{
		fmt.Sprintf("%d", int32(r.Status)),
		hex.EncodeToString(r.ResultData),
		fmt.Sprintf("%d", r.ObservedBlockHeight),
		hex.EncodeToString(r.ObservedBlockHash),
	}
}

// hashFields builds a domain-separated digest over pre-hashed parts, so a value
// containing the ":" join character cannot be made to impersonate a field boundary.
// Same construction as x/uexecutor/types/keys.go:83.
func hashFields(domain collections.Prefix, parts ...string) string {
	hashed := make([]string, 0, len(parts)+1)
	d := sha256.Sum256(domain.Bytes())
	hashed = append(hashed, hex.EncodeToString(d[:]))
	for _, p := range parts {
		sum := sha256.Sum256([]byte(p))
		hashed = append(hashed, hex.EncodeToString(sum[:]))
	}
	final := sha256.Sum256([]byte(strings.Join(hashed, ":")))
	return hex.EncodeToString(final[:])
}
