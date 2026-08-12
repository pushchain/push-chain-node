package common

// Safe fallback confirmation depths used when the registry configures 0 and
// instant routes are not enabled.
const (
	DefaultFastConfirmations     uint64 = 5
	DefaultStandardConfirmations uint64 = 12
)

// ConfirmationDepth returns latestHeight - txHeight + 1, the confirmation count
// with the inclusion block counted as one. ok is false when latestHeight <
// txHeight (a cross-RPC height skew); callers must defer rather than trust the
// depth, since the unchecked subtraction would underflow.
func ConfirmationDepth(latestHeight, txHeight uint64) (depth uint64, ok bool) {
	if latestHeight < txHeight {
		return 0, false
	}
	return latestHeight - txHeight + 1, true
}
