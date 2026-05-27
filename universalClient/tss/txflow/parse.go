package txflow

import (
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/pushchain/push-chain-node/universalClient/chains/common"
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
		SigningHash:            signingHash,
		Nonce:                  sd.Nonce,
		TSSFundMigrationAmount: sd.TSSFundMigrationAmount,
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

// ReadFundMigrationSigner derives the sender EVM address (old TSS) and reads
// the signed nonce from a fund migration event payload. Returns ok=false on
// missing/invalid fields — caller defers in that case.
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
