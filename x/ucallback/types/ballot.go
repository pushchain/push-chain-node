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

// BallotExpiryAfterBlocks converts a read request's absolute deadline into the
// relative argument VoteOnBallot expects.
//
// x/uvalidator stores BlockHeightExpiry as createdHeight + expiryAfterBlocks
// (types/ballot.go:109), so an absolute target has to be expressed as a delta from
// the height the ballot is created at.
//
// The two clocks are deliberately fused: the ballot expires exactly when the
// request does. x/uexecutor instead passes an inert 100M-block expiry to keep
// ballots alive indefinitely, but a read has a real deadline of its own — set by
// the app, enforced by the contract at UniversalCallback.sol:207 — and a ballot
// that outlived it could only ever finalize into a request no longer worth
// fulfilling.
//
// Returns at least 1 so a ballot is never created already expired. Callers reject
// past-deadline requests before reaching here; this is the backstop.
func BallotExpiryAfterBlocks(expiryHeight uint64, currentHeight int64) int64 {
	delta := int64(expiryHeight) - currentHeight
	if delta < 1 {
		return 1
	}
	return delta
}

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
