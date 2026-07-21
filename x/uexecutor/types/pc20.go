package types

import "strings"

// PC20 is the Push-native counterpart of PRC20: a Push-native ERC20 is locked in
// the on-Push VaultPC20 contract on export and a wrapper token is minted on the
// destination chain; the wrapper is burned on return and the original is
// unlocked. PC20 flows reuse the same gateway events and TxTypes as PRC20 — the
// gateway prepends a 4-byte magic selector (PC20Selector / PRC20Selector, both in
// constants.go) to the payload so the chain can tell the two apart without a new
// event or a new TxType.

// hasSelectorPrefix reports whether a 0x-prefixed, hex payload begins with the
// given 0x-stripped, lower-hex selector. Matching is done on the hex text
// (case-insensitively) so it never allocates a decoded byte slice; an empty
// selector or a too-short payload reports false.
func hasSelectorPrefix(payload, selector string) bool {
	if selector == "" {
		return false
	}
	p := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(payload)), "0x")
	if len(p) < len(selector) {
		return false
	}
	return p[:len(selector)] == selector
}

// IsPC20Payload reports whether payload begins with the PC20 selector, which is
// how the chain routes a gateway event to the PC20 path (lock/mint) versus the
// PRC20 path without needing a new event or TxType.
//
// payload is a gateway event payload as decoded by the event decoders — a
// 0x-prefixed, lower-hex string (e.g. "0x50433230..."). Anything not prefixed with
// the PC20 selector (an empty PRC20 funds-only payload, a PRC20 call, or a distinct
// PRC20 selector prefix) reports false and takes the PRC20 path, so routing is
// correct as long as the PC20 and PRC20 selectors differ.
func IsPC20Payload(payload string) bool {
	return hasSelectorPrefix(payload, PC20Selector)
}

// StripSelector removes the leading 4-byte (8 hex char) magic selector from a
// hex payload and returns the remainder as a 0x-prefixed hex string. Callers must
// have already confirmed a selector is present (via IsPC20Payload / a PRC20 match);
// a payload with no room for the selector is returned unchanged.
func StripSelector(payload string) string {
	p := strings.TrimSpace(payload)
	if strings.HasPrefix(strings.ToLower(p), "0x") {
		p = p[2:]
	}
	if len(p) < selectorHexLen {
		return payload
	}
	return "0x" + p[selectorHexLen:]
}

// RouteInboundPayload inspects an inbound raw payload's leading selector and
// returns whether it is a PC20 return (burn wrapper -> unlock locked native) and
// the user payload with any recognised selector stripped. A payload with no
// recognised selector is returned unchanged — today's un-prefixed PRC20 behaviour —
// so this stays correct both before and after the PRC20 selector ships.
func RouteInboundPayload(payload string) (isPC20 bool, userPayload string) {
	if IsPC20Payload(payload) {
		return true, StripSelector(payload)
	}
	if hasSelectorPrefix(payload, PRC20Selector) {
		return false, StripSelector(payload)
	}
	return false, payload
}
