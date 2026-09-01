package txflow

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/pushchain/push-chain-node/universalClient/externalchains/common"
	"github.com/pushchain/push-chain-node/universalClient/store"
	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

// DecodeSigningData converts the persisted hex-encoded signature + signing
// hash into the byte forms the chain-specific tx builders consume.
func DecodeSigningData(sd *SigningData) (*common.UnsignedSigningReq, []byte, error) {
	signingHash, err := hex.DecodeString(sd.SigningHash)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode signing hash: %w", err)
	}
	signature, err := hex.DecodeString(sd.Signature)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode signature: %w", err)
	}
	return &common.UnsignedSigningReq{
		SigningHash: signingHash,
		Nonce:       sd.Nonce,
	}, signature, nil
}

// ReadSignedNonce extracts the signed nonce from any signed outbound event
// payload. Returns ok=false when the payload is unparseable or signing data
// is missing — caller defers in that case.
func ReadSignedNonce(event *store.Event) (uint64, bool) {
	var data SignedOutboundData
	if err := json.Unmarshal(event.EventData, &data); err != nil || data.SigningData == nil {
		return 0, false
	}
	return data.SigningData.Nonce, true
}

// ReadSigningDeadline extracts the chain-emitted signing deadline from a
// signed outbound event payload. Returns 0 if the event is unparseable or
// the deadline was never set (legacy events).
func ReadSigningDeadline(event *store.Event) int64 {
	var data SignedOutboundData
	if err := json.Unmarshal(event.EventData, &data); err != nil {
		return 0
	}
	return data.SigningDeadline
}

// RecoverOutboundSigner returns the EVM address that actually signed a SIGNED or
// BROADCASTED outbound, recovered from the persisted signature and signing hash.
//
// Nonces are per-EOA, so a nonce check is only meaningful against the key that
// signed. Outbound SigningData carries no key id, and after a TSS rotation the
// current key is a different EOA with an unrelated nonce sequence — comparing a
// K1-signed nonce against K2's would report "consumed" while K1's nonce is still
// free. Recovering from the signature binds the check to the right key without
// persisting anything new, so events signed before this existed are covered too.
//
// Returns ok=false when the signer cannot be established, which callers must
// treat as "defer", never as evidence the transaction did not execute.
func RecoverOutboundSigner(event *store.Event) (signer string, nonce uint64, ok bool) {
	var data SignedOutboundData
	if err := json.Unmarshal(event.EventData, &data); err != nil || data.SigningData == nil {
		return "", 0, false
	}
	req, signature, err := DecodeSigningData(data.SigningData)
	if err != nil || len(signature) != 65 || len(req.SigningHash) != 32 {
		return "", 0, false
	}
	pub, err := crypto.SigToPub(req.SigningHash, signature)
	if err != nil || pub == nil {
		return "", 0, false
	}
	addr, err := coordinator.DeriveEVMAddressFromPubkey(hex.EncodeToString(crypto.CompressPubkey(pub)))
	if err != nil {
		return "", 0, false
	}
	return addr, data.SigningData.Nonce, true
}

// ReadFundMigrationSigner derives the sender EVM address (old TSS) and reads
// the signed nonce from a fund migration event payload. Returns ok=false on
// missing/invalid fields, and the caller defers in that case.
func ReadFundMigrationSigner(event *store.Event) (signer string, nonce uint64, ok bool) {
	var data SignedFundMigrationData
	if err := json.Unmarshal(event.EventData, &data); err != nil || data.SigningData == nil || data.OldTssPubkey == "" {
		return "", 0, false
	}
	addr, err := coordinator.DeriveEVMAddressFromPubkey(data.OldTssPubkey)
	if err != nil {
		return "", 0, false
	}
	return addr, data.SigningData.Nonce, true
}
