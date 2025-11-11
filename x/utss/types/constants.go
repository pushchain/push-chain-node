package types

// Quorum numerator/denominator for validator votes (>2/3)
const (
	VotesThresholdNumerator   = 2
	VotesThresholdDenominator = 3

	// Default number of blocks after which tss process expires
	DefaultTssProcessExpiryAfterBlocks = 1000
)
