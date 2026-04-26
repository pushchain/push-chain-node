package utils

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"

	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"

	authz "github.com/cosmos/cosmos-sdk/x/authz"
)

func ExecVoteInbound(
	t *testing.T,
	ctx sdk.Context,
	app *app.ChainApp,
	universalAddr string,
	coreValAddr string,
	inbound *uexecutortypes.Inbound,
) error {
	t.Helper()

	// Auto-convert: if tests set UniversalPayload but not RawPayload,
	// encode it into RawPayload (simulating the new UV format where UV sends raw bytes).
	if inbound.UniversalPayload != nil && inbound.RawPayload == "" {
		namespace := strings.Split(inbound.SourceChain, ":")[0]
		var raw string
		var err error
		switch namespace {
		case "solana":
			raw, err = EncodeUniversalPayloadBorsh(inbound.UniversalPayload)
		default:
			raw, err = uexecutortypes.EncodeUniversalPayloadToRaw(inbound.UniversalPayload)
		}
		if err != nil {
			return fmt.Errorf("test helper: failed to encode universal_payload to raw: %w", err)
		}
		inbound.RawPayload = raw
		inbound.UniversalPayload = nil
	}

	// Core validator account (string bech32) signs the vote
	msg := &uexecutortypes.MsgVoteInbound{
		Signer:  coreValAddr,
		Inbound: inbound,
	}

	// Universal validator executes it via MsgExec
	execMsg := authz.NewMsgExec(
		sdk.MustAccAddressFromBech32(universalAddr), // universal validator
		[]sdk.Msg{msg},
	)

	_, err := app.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}

func ExecVoteOutbound(
	t *testing.T,
	ctx sdk.Context,
	app *app.ChainApp,
	universalAddr string, // universal validator (grantee)
	coreValAddr string, // core validator (signer)
	utxId string, // universal tx id
	outbound *uexecutortypes.OutboundTx,
	success bool,
	errorMsg string,
	gasFeeUsed string, // actual gas fee consumed on destination chain; "" means no refund
) error {
	t.Helper()

	observed := &uexecutortypes.OutboundObservation{
		Success:     success,
		ErrorMsg:    errorMsg,
		TxHash:      fmt.Sprintf("0xobserved-%s", outbound.Id),
		BlockHeight: 1,
		GasFeeUsed:  gasFeeUsed,
	}

	msg := &uexecutortypes.MsgVoteOutbound{
		Signer:     coreValAddr,
		TxId:       outbound.Id,
		UtxId:      utxId,
		ObservedTx: observed,
	}

	execMsg := authz.NewMsgExec(
		sdk.MustAccAddressFromBech32(universalAddr),
		[]sdk.Msg{msg},
	)

	_, err := app.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}

// EncodeUniversalPayloadBorsh Borsh-encodes a UniversalPayload into a hex string.
// Matches the Rust Anchor/Borsh layout used by the Solana gateway program.
func EncodeUniversalPayloadBorsh(up *uexecutortypes.UniversalPayload) (string, error) {
	if up == nil {
		return "", nil
	}

	// to: [u8; 20]
	var to [20]byte
	toClean := strings.TrimPrefix(up.To, "0x")
	if toClean != "" {
		toBytes, err := hex.DecodeString(toClean)
		if err != nil {
			return "", fmt.Errorf("invalid to hex: %w", err)
		}
		copy(to[20-len(toBytes):], toBytes) // right-align like address
	}

	// value: u64
	value, err := strconv.ParseUint(up.Value, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid value: %w", err)
	}

	// data: Vec<u8>
	var dataBytes []byte
	dataClean := strings.TrimPrefix(up.Data, "0x")
	if dataClean != "" {
		dataBytes, err = hex.DecodeString(dataClean)
		if err != nil {
			return "", fmt.Errorf("invalid data hex: %w", err)
		}
	}

	gasLimit, _ := strconv.ParseUint(up.GasLimit, 10, 64)
	maxFeePerGas, _ := strconv.ParseUint(up.MaxFeePerGas, 10, 64)
	maxPriorityFeePerGas, _ := strconv.ParseUint(up.MaxPriorityFeePerGas, 10, 64)
	nonce, _ := strconv.ParseUint(up.Nonce, 10, 64)
	deadline, _ := strconv.ParseInt(up.Deadline, 10, 64)

	buf := make([]byte, 0, 20+8+4+len(dataBytes)+8*5+1)

	buf = append(buf, to[:]...)

	tmp := make([]byte, 8)
	binary.LittleEndian.PutUint64(tmp, value)
	buf = append(buf, tmp...)

	lenBuf := make([]byte, 4)
	binary.LittleEndian.PutUint32(lenBuf, uint32(len(dataBytes)))
	buf = append(buf, lenBuf...)
	buf = append(buf, dataBytes...)

	binary.LittleEndian.PutUint64(tmp, gasLimit)
	buf = append(buf, tmp...)

	binary.LittleEndian.PutUint64(tmp, maxFeePerGas)
	buf = append(buf, tmp...)

	binary.LittleEndian.PutUint64(tmp, maxPriorityFeePerGas)
	buf = append(buf, tmp...)

	binary.LittleEndian.PutUint64(tmp, nonce)
	buf = append(buf, tmp...)

	binary.LittleEndian.PutUint64(tmp, uint64(deadline))
	buf = append(buf, tmp...)

	buf = append(buf, uint8(up.VType))

	return "0x" + hex.EncodeToString(buf), nil
}

// ExecVoteChainMeta executes a MsgVoteChainMeta on behalf of the core validator
// through the universal validator using authz Exec.
func ExecVoteChainMeta(
	t *testing.T,
	ctx sdk.Context,
	app *app.ChainApp,
	universalAddr string,
	coreValAddr string,
	chainID string,
	price uint64,
	blockNumber uint64,
) error {
	t.Helper()

	msg := &uexecutortypes.MsgVoteChainMeta{
		Signer:          coreValAddr,
		ObservedChainId: chainID,
		Price:           price,
		ChainHeight:     blockNumber,
	}

	execMsg := authz.NewMsgExec(
		sdk.MustAccAddressFromBech32(universalAddr),
		[]sdk.Msg{msg},
	)

	_, err := app.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}
