package integrationtest

import (
	"encoding/binary"
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// forwarderCode assembles runtime bytecode that CALLs target with value and a
// fixed calldata blob appended to the code, reverting if the inner call fails.
// It ignores its own calldata, so it answers executeUniversalTx like anything else.
func forwarderCode(target common.Address, value *big.Int, data []byte) string {
	l := len(data)
	push2 := func(n int) []byte { b := []byte{0x61, 0, 0}; binary.BigEndian.PutUint16(b[1:], uint16(n)); return b }

	var p []byte
	// CODECOPY(destOffset=0, codeOffset=blobOff, length=l)
	p = append(p, push2(l)...)
	blobOffPos := len(p) + 1 // patched once the prologue length is known
	p = append(p, push2(0)...)
	p = append(p, 0x60, 0x00, 0x39)

	// CALL(gas, target, value, 0, l, 0, 0) — push args in reverse order
	p = append(p, 0x60, 0x00) // retLength
	p = append(p, 0x60, 0x00) // retOffset
	p = append(p, push2(l)...)
	p = append(p, 0x60, 0x00) // argsOffset
	v := make([]byte, 32)
	value.FillBytes(v)
	p = append(p, 0x7f)
	p = append(p, v...)
	p = append(p, 0x73)
	p = append(p, target.Bytes()...)
	p = append(p, 0x5a, 0xf1) // GAS, CALL

	// success ? STOP : REVERT
	dest := len(p) + 8
	p = append(p, 0x60, byte(dest), 0x57) // PUSH1 dest, JUMPI
	p = append(p, 0x60, 0x00, 0x60, 0x00, 0xfd)
	p = append(p, 0x5b, 0x00) // JUMPDEST, STOP

	binary.BigEndian.PutUint16(p[blobOffPos:], uint16(len(p)))
	return common.Bytes2Hex(append(p, data...))
}

// N1 end-to-end: a CEA inbound whose recipient is a contract runs through
// CallExecuteUniversalTx. When that contract requests a read, the event is
// emitted by UniversalCallback — and must end up recorded in x/ucallback.
//
// Derived calls never fire the EVM post-tx hook, so before the hand-off in
// CallExecuteUniversalTx the request was emitted and its budget escrowed with
// nothing recording it, leaving the funds unreachable.
func TestReadRequestedFromCEAContract_IsIngested(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	uek := chainApp.UexecutorKeeper
	uck := chainApp.UcallbackKeeper

	callback := utils.SetupUniversalCallback(t, chainApp, ctx)
	core := utils.SetupMockUniversalCoreForReads(t, chainApp, ctx)
	chainApp.EVMKeeper.SetState(ctx, callback,
		common.BigToHash(big.NewInt(0)), common.BytesToHash(core.Bytes()).Bytes())

	deposit := big.NewInt(4_000_000_000_000_000)

	reqABI := loadRequestABI(t)
	callData, err := reqABI.Pack("requestExternalReadSelf",
		readSpecArg{
			Account: accountArg{
				ChainNamespace: "eip155",
				ChainId:        "11155111",
				Owner:          common.FromHex("0x1111111111111111111111111111111111111111"),
			},
			Query:                 common.FromHex("0xdeadbeef"),
			MinConfirmations:      uint16(6),
			BlockNumber:           uint64(8_000_000),
			ExpiryPushChainHeight: uint64(ctx.BlockHeight()) + 500,
			MaxFee:                new(big.Int).Mul(deposit, big.NewInt(2)),
			RevertRecipient:       common.HexToAddress("0x00000000000000000000000000000000000BEEF1"),
		},
		[4]byte{0x11, 0x22, 0x33, 0x44},
		uint64(250_000),
	)
	require.NoError(t, err)

	// The CEA recipient: forwards into UniversalCallback, paying the deposit.
	recipient := common.HexToAddress("0x00000000000000000000000000000000000CEA01")
	utils.DeployContract(t, chainApp, ctx, recipient, forwarderCode(callback, deposit, callData))
	fund(t, chainApp, ctx, sdk.AccAddress(recipient.Bytes()), new(big.Int).Mul(deposit, big.NewInt(10)))

	var txId [32]byte
	copy(txId[:], common.FromHex("0xabcd"))

	res, err := uek.CallExecuteUniversalTx(
		ctx, recipient, "eip155:11155111",
		common.FromHex("0x1111111111111111111111111111111111111111"),
		common.FromHex("0xdeadbeef"), big.NewInt(0),
		utils.GetDefaultAddresses().PRC20USDCAddr, txId,
	)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.VmError, "the CEA contract call must not revert: %s", res.VmError)
	require.NotEmpty(t, res.Logs, "UniversalCallback must have emitted ReadRequested")

	var recorded []ucallbacktypes.UniversalRead
	require.NoError(t, uck.IterateReadsByTxHash(ctx, res.Hash,
		func(ur ucallbacktypes.UniversalRead) bool {
			recorded = append(recorded, ur)
			return false
		}))

	require.Len(t, recorded, 1,
		"the read must be recorded; without the hand-off in CallExecuteUniversalTx "+
			"the hook never fires for a derived call and the escrowed budget is stranded (N1)")
	require.Equal(t, uint64(250_000), recorded[0].Request.CallbackGasLimit)
}
