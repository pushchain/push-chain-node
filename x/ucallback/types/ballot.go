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
// error_code is included: disagreement about WHY a read failed is real
// disagreement. One validator reporting REVERTED and another NOT_FOUND saw
// different things, and splitting the ballot is the correct outcome — letting both
// collapse onto a bare ERROR would paper over it.
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
		fmt.Sprintf("%d", int32(r.ErrorCode)),
		hex.EncodeToString(r.ResultData),
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

// ValidateReadResult rejects observations that cannot be honest, before they reach
// a ballot.
//
// These are not defensive niceties: each rejected shape would produce a ballot key
// that no other validator observing the same thing could reach, so an accepted one
// would sit alone and never reach quorum. Failing loudly at submission turns a
// silent stall into an error the operator can see.
func ValidateReadResult(r *ReadResult) error {
	if r == nil {
		return fmt.Errorf("read result is required")
	}

	switch r.Status {
	case ReadStatus_READ_STATUS_SUCCESS:
		if r.ErrorCode != ReadErrorCode_READ_ERROR_UNSPECIFIED {
			return fmt.Errorf("successful read must not carry error code %s", r.ErrorCode)
		}

	case ReadStatus_READ_STATUS_ERROR:
		// result_data must be empty. A failed read has no payload to deliver, and
		// error detail differs per provider — one validator attaching a revert
		// blob and another attaching nothing would split the ballot.
		if len(r.ResultData) != 0 {
			return fmt.Errorf("failed read must not carry result data (%d bytes)", len(r.ResultData))
		}

	default:
		return fmt.Errorf("read status %s is not a valid observation", r.Status)
	}

	// Reserved for v2 MEDIAN; a v1 validator populating it is running code this
	// chain cannot interpret, and it is excluded from the ballot key so the
	// divergence would be invisible.
	if len(r.Aggregates) != 0 {
		return fmt.Errorf("aggregates are not supported in v1")
	}

	return nil
}
