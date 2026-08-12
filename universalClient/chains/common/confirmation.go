package common

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
