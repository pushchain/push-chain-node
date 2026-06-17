package types

import (
	"crypto/sha256"
	"encoding/hex"
	fmt "fmt"
	"strings"

	"cosmossdk.io/collections"

	"github.com/pushchain/push-chain-node/utils"
)

var (
	// ParamsKey saves the current module params.
	ParamsKey = collections.NewPrefix(0)

	// ParamsName is the name of the params collection.
	ParamsName = "params"

	// Old storage in V1
	ChainConfigsKey  = collections.NewPrefix(1) // ChainConfigsKey saves the current module chainConfigs collection prefix
	ChainConfigsName = "chain_configs"          // ChainConfigsName is the name of the chainConfigs collection.

	// PendingInboundsKey stores the per-variant audit trail of in-flight
	// inbounds (Map[utx_key → PendingInboundEntry]). See
	// plan-pending-inbound-cleanup.md.
	PendingInboundsKey  = collections.NewPrefix(2)
	PendingInboundsName = "pending_inbounds"

	UniversalTxKey  = collections.NewPrefix(3)
	UniversalTxName = "universal_tx"

	ModuleAccountNonceKey  = collections.NewPrefix(4)
	ModuleAccountNonceName = "module_account_nonce"

	GasPricesKey  = collections.NewPrefix(5)
	GasPricesName = "gas_prices"

	ChainMetaKey   = collections.NewPrefix(6)
	ChainMetasName = "chain_metas"

	PendingOutboundsKey  = collections.NewPrefix(7)
	PendingOutboundsName = "pending_outbounds"

	// ExpiredInboundsKey stores the per-variant audit trail of inbounds
	// whose ballots all reached a terminal-failure state (EXPIRED/REJECTED)
	// without producing a UniversalTx. Consumed by the future escape-hatch
	// refund flow. See plan-pending-inbound-cleanup.md.
	ExpiredInboundsKey  = collections.NewPrefix(8)
	ExpiredInboundsName = "expired_inbounds"

	// Domain separators for the canonical ballot-key digests. Hashed into the
	// key preimage (never used as store prefixes); kept in this block so prefix
	// numbers stay unique. They keep inbound vs outbound keys disjoint in the
	// shared uvalidator Ballots map.
	InboundBallotDomain  = collections.NewPrefix(9)
	OutboundBallotDomain = collections.NewPrefix(10)
)

const (
	ModuleName = "uexecutor"

	StoreKey = ModuleName

	QuerierRoute = ModuleName
)

// GetInboundUniversalTxKey: UTX identity from canonical (source_chain, tx_hash,
// log_index). Canonicalizes locals; caller's inbound is not mutated.
func GetInboundUniversalTxKey(inbound Inbound) string {
	chain := strings.TrimSpace(inbound.SourceChain)
	txHash := utils.LenientCanonicalizeTxHash(chain, inbound.TxHash)
	logIndex := strings.TrimSpace(inbound.LogIndex)
	data := fmt.Sprintf("%s:%s:%s", chain, txHash, logIndex)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:]) // hash[:] converts [32]byte → []byte
}

// hashFields = sha256( hex(sha256(domain)) : hex(sha256(f0)) : ... ). Per-field
// hashing makes it injective — a sub-hash can't contain ':', so no field value
// can shift a boundary and collide with a different tuple.
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

// GetInboundBallotKey: versioned canonical digest over every execution-
// relevant field (so quorum implies agreement on the outcome), excluding
// universal_payload (recomputed on-chain from raw_payload). Self-canonicalizes,
// so any caller gets one ballot per logical event.
func GetInboundBallotKey(inbound Inbound) (string, error) {
	chain := strings.TrimSpace(inbound.SourceChain)

	// nil RevertInstructions and an empty FundRecipient are semantically
	// identical (revert falls back to sender) — digest them identically.
	fundRecipient := ""
	if inbound.RevertInstructions != nil {
		fundRecipient = utils.LenientCanonicalizeAddress(chain, inbound.RevertInstructions.FundRecipient)
	}

	return hashFields(
		InboundBallotDomain,
		chain,
		utils.LenientCanonicalizeTxHash(chain, inbound.TxHash),
		strings.TrimSpace(inbound.LogIndex),
		utils.LenientCanonicalizeAddress(chain, inbound.Sender),
		// Recipient lives on Push Chain (EVM) regardless of source chain.
		utils.LenientCanonicalizeEVMAddress(inbound.Recipient),
		strings.TrimSpace(inbound.Amount),
		utils.LenientCanonicalizeAddress(chain, inbound.AssetAddr),
		fmt.Sprintf("%d", inbound.TxType),
		utils.CanonicalizeHexBlob(inbound.VerificationData),
		fundRecipient,
		fmt.Sprintf("%t", inbound.IsCEA),
		utils.CanonicalizeHexBlob(inbound.RawPayload),
		// universal_payload intentionally excluded (derived, ignored on-chain).
	), nil
}

func GetPcUniversalTxKey(pcCaip string, pc PCTx) string {
	data := fmt.Sprintf("%s:%s", pcCaip, pc.TxHash)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GetOutboundBallotKey: versioned canonical digest over all observation fields
// (all consensus-critical — gas_fee_used drives the refund, error_msg must be
// agreed so no voter can inject unconsensused text). Caller canonicalizes
// tx_hash for the destination chain at vote ingress.
func GetOutboundBallotKey(
	utxId string,
	outboundIndex string,
	observedTx OutboundObservation,
) (string, error) {
	return hashFields(
		OutboundBallotDomain,
		utxId,
		outboundIndex,
		fmt.Sprintf("%t", observedTx.Success),
		fmt.Sprintf("%d", observedTx.BlockHeight),
		observedTx.TxHash,
		observedTx.GasFeeUsed,
		observedTx.ErrorMsg,
	), nil
}

// GetOutboundRevertId generates a deterministic outbound ID for an inbound-revert
// outbound. sourceChain is the CAIP-2 identifier of the chain the inbound came from
// (e.g. "eip155:1"); logIndex disambiguates multiple bridge events in the same
// source tx. This ID is also used as the subTxId on the source-chain gateway call,
// providing replay protection — so it must be unique per inbound event.
func GetOutboundRevertId(sourceChain, inboundTxHash, logIndex string) string {
	data := fmt.Sprintf("%s:%s:%s:REVERT", sourceChain, inboundTxHash, logIndex)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GetRescueFundsOutboundId generates a deterministic outbound ID for a rescue-funds
// outbound. pushChainCaip is the CAIP-2 identifier of Push Chain (e.g. "eip155:2240"),
// mirroring the convention used by GetPcUniversalTxKey. This ID is also used as the
// subTxId on the source-chain gateway call, providing replay protection.
func GetRescueFundsOutboundId(pushChainCaip string, pcTxHash string, logIndex string) string {
	data := fmt.Sprintf("%s:%s:%s:RESCUE", pushChainCaip, pcTxHash, logIndex)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
