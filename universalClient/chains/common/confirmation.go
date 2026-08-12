package common

// ConfirmationDepth returns the number of confirmations for a transaction
// observed at txHeight against a chain tip at latestHeight, defined as
// latestHeight - txHeight + 1 (the inclusion block counts as one confirmation).
//
// ok is false when latestHeight < txHeight. That ordering is not physically
// possible on a single consistent view of a chain, but the latest-height and
// transaction reads are independent RPC calls that the pool round-robins across
// endpoints. When the endpoint serving the transaction is ahead of the one
// serving the tip, an unchecked latestHeight - txHeight underflows uint64 to a
// value near 2^64 and satisfies any confirmation threshold, prematurely
// finalizing an inbound. Callers must treat ok == false as "defer, still
// pending" rather than trusting the returned depth.
func ConfirmationDepth(latestHeight, txHeight uint64) (depth uint64, ok bool) {
	if latestHeight < txHeight {
		return 0, false
	}
	return latestHeight - txHeight + 1, true
}
