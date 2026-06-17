package utils

import (
	"encoding/hex"
	"fmt"
	"strings"

	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/mr-tron/base58"
)

// Per-CAIP-2-namespace canonical forms for ballot/storage keys: eip155
// addresses → EIP-55, eip155 hashes → 0x-lowercase, solana → base58 preserved
// (case-significant) / hex lowercased, other → trimmed. One form per value so
// encoding variance can't fragment votes or duplicate rows.
const (
	namespaceEVM    = "eip155"
	namespaceSolana = "solana"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// CAIP2Namespace returns the namespace component of a CAIP-2 chain id
// ("eip155:1" → "eip155"). Returns "" when the id has no namespace.
func CAIP2Namespace(chain string) string {
	parts := strings.SplitN(strings.TrimSpace(chain), ":", 2)
	if len(parts) != 2 {
		return ""
	}
	return parts[0]
}

func isHexString(s string) bool {
	if s == "" {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func isBase58String(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !strings.ContainsRune(base58Alphabet, c) {
			return false
		}
	}
	return true
}

// strip0x removes a leading "0x"/"0X" and reports whether one was present.
func strip0x(s string) (string, bool) {
	if len(s) >= 2 && s[0] == '0' && (s[1] == 'x' || s[1] == 'X') {
		return s[2:], true
	}
	return s, false
}

// CanonicalizeEVMAddress validates s as a 20-byte hex address (with or
// without 0x) and returns the EIP-55 checksummed, 0x-prefixed form.
func CanonicalizeEVMAddress(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !ethcommon.IsHexAddress(s) {
		return "", fmt.Errorf("invalid EVM address %q: must be 20-byte hex", s)
	}
	return ethcommon.HexToAddress(s).Hex(), nil
}

// CanonicalizeEVMHash validates s as a 32-byte hex hash (with or without 0x)
// and returns the 0x-prefixed lowercase form.
func CanonicalizeEVMHash(s string) (string, error) {
	s = strings.TrimSpace(s)
	body, _ := strip0x(s)
	if len(body) != 64 || !isHexString(body) {
		return "", fmt.Errorf("invalid EVM tx hash %q: must be 32-byte hex", s)
	}
	return "0x" + strings.ToLower(body), nil
}

// CanonicalizeHexBlob lenient-canonicalizes free-length hex payloads
// (raw_payload, verification_data): valid hex (with or without 0x, even
// length) → 0x-prefixed lowercase; anything else is returned trimmed as-is.
func CanonicalizeHexBlob(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	body, _ := strip0x(s)
	if len(body)%2 == 0 && isHexString(body) {
		return "0x" + strings.ToLower(body)
	}
	return s
}

// canonicalizeSolanaValue: hex inputs lowercase (0x kept); base58 inputs are
// charset-validated and preserved as-is (base58 is case-significant —
// lowercasing would corrupt the value).
func canonicalizeSolanaValue(s string) (string, error) {
	s = strings.TrimSpace(s)
	if body, had0x := strip0x(s); had0x && len(body)%2 == 0 && isHexString(body) {
		return "0x" + strings.ToLower(body), nil
	}
	if isBase58String(s) {
		return s, nil
	}
	return "", fmt.Errorf("invalid solana value %q: neither 0x-hex nor base58", s)
}

// canonicalizeSolanaTxHash additionally unifies base58-encoded 64-byte
// signatures into 0x-lowercase-hex, matching the form the reference client
// submits, so hex and base58 encodings of the same signature converge.
func canonicalizeSolanaTxHash(s string) (string, error) {
	canon, err := canonicalizeSolanaValue(s)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(canon, "0x") {
		return canon, nil
	}
	if raw, decErr := base58.Decode(canon); decErr == nil && len(raw) == 64 {
		return "0x" + hex.EncodeToString(raw), nil
	}
	return canon, nil
}

// CanonicalizeAddressByNamespace canonicalizes an address for the given
// CAIP-2 chain. Empty input passes through (optional fields).
func CanonicalizeAddressByNamespace(chain, addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", nil
	}
	switch CAIP2Namespace(chain) {
	case namespaceEVM:
		return CanonicalizeEVMAddress(addr)
	case namespaceSolana:
		return canonicalizeSolanaValue(addr)
	default:
		return addr, nil
	}
}

// CanonicalizeTxHashByNamespace canonicalizes a transaction hash/signature
// for the given CAIP-2 chain. Empty input passes through (e.g. failed
// outbound observations carry no hash).
func CanonicalizeTxHashByNamespace(chain, txHash string) (string, error) {
	txHash = strings.TrimSpace(txHash)
	if txHash == "" {
		return "", nil
	}
	switch CAIP2Namespace(chain) {
	case namespaceEVM:
		return CanonicalizeEVMHash(txHash)
	case namespaceSolana:
		return canonicalizeSolanaTxHash(txHash)
	default:
		return txHash, nil
	}
}

// Lenient variants for the vote-ingress / key-derivation path: canonical form
// when the value parses, trimmed input otherwise (never an error). Used there
// because that path must never drop a vote — a malformed inbound still has to
// produce an on-chain UTX record (with a failed PCTx / revert), and
// execution-level validation, not key derivation, is what rejects it. Honest
// observers of the same value still converge (same trimmed string), and the
// injective hashFields digest keeps even malformed values collision-safe.
// Strict (error-returning) variants are for admin/config paths where bad input
// should be rejected before it is persisted.

// LenientCanonicalizeAddress canonicalizes addr for chain, falling back to
// the trimmed input when it does not parse.
func LenientCanonicalizeAddress(chain, addr string) string {
	canon, err := CanonicalizeAddressByNamespace(chain, addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return canon
}

// LenientCanonicalizeTxHash canonicalizes txHash for chain, falling back to
// the trimmed input when it does not parse.
func LenientCanonicalizeTxHash(chain, txHash string) string {
	canon, err := CanonicalizeTxHashByNamespace(chain, txHash)
	if err != nil {
		return strings.TrimSpace(txHash)
	}
	return canon
}

// LenientCanonicalizeEVMAddress canonicalizes a Push-Chain (EVM) address to
// EIP-55, falling back to the trimmed input when it does not parse.
func LenientCanonicalizeEVMAddress(addr string) string {
	canon, err := CanonicalizeEVMAddress(addr)
	if err != nil {
		return strings.TrimSpace(addr)
	}
	return canon
}
