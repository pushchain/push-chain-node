package types

import "strings"

// PC20 is the Push-native counterpart of PRC20: a Push-native ERC20 is locked in
// the on-Push VaultPC20 contract on export and a wrapper token is minted on the
// destination chain; the wrapper is burned on return and the original is
// unlocked. PC20 flows reuse the same gateway events and TxTypes as PRC20 — the
// gateway prepends a 4-byte magic selector to the event payload so the chain can
// tell the two apart without a new event or a new TxType.
//
// PC20Selector is the ASCII encoding of "PC20" (0x50433230). It is matched
// against the leading 4 bytes of a gateway event payload.
const PC20Selector = "50433230"

// pc20SelectorHexLen is the length of the selector in a 0x-stripped hex string
// (4 bytes -> 8 hex characters).
const pc20SelectorHexLen = len(PC20Selector)

// IsPC20Payload reports whether payload begins with the PC20 selector, which is
// how the chain routes a gateway event to the PC20 path (lock/mint) versus the
// PRC20 path without needing a new event or TxType.
//
// payload is a gateway event payload as decoded by the event decoders — a
// 0x-prefixed, lower-hex string (e.g. "0x50433230..."). Matching is done on the
// hex text (case-insensitively) so it never allocates a decoded byte slice, and
// an empty or too-short payload reports false. Anything not prefixed with the
// PC20 selector (an empty PRC20 funds-only payload, a PRC20 call, or a distinct
// PRC20 selector prefix) reports false and takes the PRC20 path, so routing is
// correct as long as the PC20 and PRC20 selectors differ.
func IsPC20Payload(payload string) bool {
	p := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(payload)), "0x")
	if len(p) < pc20SelectorHexLen {
		return false
	}
	return p[:pc20SelectorHexLen] == PC20Selector
}
