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

// IsPC20Payload reports whether payload begins with the PC20 selector.
//
// payload is a gateway event payload as decoded by the event decoders — a
// 0x-prefixed, lower-hex string (e.g. "0x50433230..."). Matching is done on the
// hex text (case-insensitively) so it never has to allocate a decoded byte
// slice, and an empty or malformed payload simply reports false. A PRC20
// funds-only payload is empty and a PRC20 call payload is an ABI tuple that
// starts with 0x00000020..., so neither collides with the selector.
func IsPC20Payload(payload string) bool {
	p := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(payload)), "0x")
	if len(p) < pc20SelectorHexLen {
		return false
	}
	return p[:pc20SelectorHexLen] == PC20Selector
}
