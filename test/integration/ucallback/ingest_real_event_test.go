package integrationtest

import (
	"math/big"
	"os"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	evmtypes "github.com/cosmos/evm/x/vm/types"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
)

// The full inbound half: the contract creates a request, emits ReadRequested, and
// we ingest that log into a UniversalRead.
//
// This is the one test where the event we decode was produced by the contract
// rather than by our own encoder. Every other event test round-trips through the
// same ABI fragment on both sides, so a wrong fragment agrees with itself — which
// is exactly how callbackGasLimit sat in the wrong position undetected.
func TestIngest_DecodesAContractEmittedEvent(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	contract := utils.SetupUniversalCallback(t, chainApp, ctx)
	core := utils.SetupMockUniversalCoreForReads(t, chainApp, ctx)

	// _universalCore is storage slot 0; the fixture writes runtime code directly,
	// so initialize() never ran to set it.
	chainApp.EVMKeeper.SetState(ctx, contract,
		common.BigToHash(big.NewInt(0)), common.BytesToHash(core.Bytes()).Bytes())

	requester := provisionEOA(t, chainApp, ctx, "0x00000000000000000000000000000000000A11CE")
	deposit := big.NewInt(4_000_000_000_000_000)
	fund(t, chainApp, ctx, sdk.AccAddress(requester.Bytes()), new(big.Int).Mul(deposit, big.NewInt(10)))

	const (
		wantGasLimit = uint64(250_000)
		wantMinConf  = uint16(6)
		wantBlockNum = uint64(8_000_000)
	)
	wantExpiry := uint64(ctx.BlockHeight()) + 500
	revertRecipient := common.HexToAddress("0x00000000000000000000000000000000000BEEF1")

	reqABI := loadRequestABI(t)
	data, err := reqABI.Pack("requestExternalReadSelf",
		readSpecArg{
			Account: accountArg{
				ChainNamespace: "eip155",
				ChainId:        "11155111",
				Owner:          common.FromHex("0x1111111111111111111111111111111111111111"),
			},
			Query:                 common.FromHex("0xdeadbeef"),
			MinConfirmations:      wantMinConf,
			BlockNumber:           wantBlockNum,
			ExpiryPushChainHeight: wantExpiry,
			MaxFee:                new(big.Int).Mul(deposit, big.NewInt(2)),
			RevertRecipient:       revertRecipient,
		},
		[4]byte{0x11, 0x22, 0x33, 0x44},
		wantGasLimit,
	)
	require.NoError(t, err)

	res, err := chainApp.EVMKeeper.DerivedEVMCallWithData(
		ctx, requester, &contract, data,
		true /* commit */, false, false,
		deposit, big.NewInt(2_000_000), nil,
	)
	require.NoError(t, err, "creating a read request must succeed: %v", res)
	require.NotEmpty(t, res.Logs, "the contract must have emitted ReadRequested")

	// --- the part under test -----------------------------------------------------
	require.NoError(t, k.IngestReadRequests(ctx, res))

	var recorded []ucallbacktypes.UniversalRead
	require.NoError(t, k.IterateReadsByTxHash(ctx, res.Hash,
		func(ur ucallbacktypes.UniversalRead) bool {
			recorded = append(recorded, ur)
			return false
		}))
	require.Len(t, recorded, 1,
		"exactly one read must be ingested from a contract-emitted ReadRequested")

	ur := recorded[0]
	req := ur.Request
	require.Equal(t, ucallbacktypes.UniversalReadStatus_UNIVERSAL_READ_STATUS_PENDING, ur.Status)

	// Field-by-field against what we asked the contract for. A signature that
	// decodes but misaligns would show up right here as scrambled values.
	require.Equal(t, wantGasLimit, req.CallbackGasLimit, "callbackGasLimit")
	require.Equal(t, uint32(wantMinConf), req.MinConfirmations, "minConfirmations")
	require.Equal(t, wantBlockNum, req.DestinationBlockHeight, "blockNumber")
	require.Equal(t, wantExpiry, req.ExpiryBlockHeight, "expiryPushChainHeight")
	require.Equal(t, "eip155:11155111", req.DestinationChain, "chain")
	require.Equal(t, requester.Hex(), req.CallbackTarget, "callbackTarget")
	require.Equal(t, requester.Hex(), req.OriginalFunder, "originalFunder")
	require.Equal(t, revertRecipient.Hex(), req.RevertRecipient, "revertRecipient")
	require.Equal(t, deposit.String(), req.FeesDeposited, "totalPaid")

	// the mock prices reads at zero, so the whole deposit is callback budget
	require.Equal(t, "0", req.ProtocolFee, "protocolFee")
	require.Equal(t, deposit.String(), req.CallbackBudget, "callbackBudget")

	// and the contract must agree this request is live and escrowed
	views := loadViewABI(t)
	id, ok := new(big.Int).SetString(strings.TrimPrefix(req.RequestId, "0x"), 16)
	require.True(t, ok)
	require.Equal(t, uint8(1), // PENDING
		staticCall(t, chainApp, ctx, views, contract, "statusOf", id)[0].(uint8))
	require.Zero(t, deposit.Cmp(
		staticCall(t, chainApp, ctx, views, contract, "totalEscrowed")[0].(*big.Int)),
		"the deposit must be escrowed on the contract")

	// ingest is idempotent -- the same receipt replayed must not duplicate
	require.NoError(t, k.IngestReadRequests(ctx, res))
	var again int
	require.NoError(t, k.IterateReadsByTxHash(ctx, res.Hash,
		func(ucallbacktypes.UniversalRead) bool { again++; return false }))
	require.Equal(t, 1, again, "replaying a receipt must not create a second record")
}

// A ReadRequested-shaped log from an address that is NOT UniversalCallback must be
// ignored. Without the address check anyone could mint read requests by emitting a
// matching log from their own contract.
func TestIngest_IgnoresLogsFromOtherContracts(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	k := chainApp.UcallbackKeeper

	forged := &evmtypes.MsgEthereumTxResponse{
		Hash: "0xforged",
		Logs: []*evmtypes.Log{{
			Address: "0x000000000000000000000000000000000000dEaD",
			Topics: []string{
				ucallbacktypes.ReadRequestedEventSig.Hex(),
				common.BigToHash(big.NewInt(1)).Hex(),
				common.BytesToHash(common.FromHex("0x02")).Hex(),
				common.BytesToHash(common.FromHex("0x03")).Hex(),
			},
			Data: []byte{},
		}},
	}
	require.NoError(t, k.IngestReadRequests(ctx, forged))

	var n int
	require.NoError(t, k.IterateReadsByTxHash(ctx, "0xforged",
		func(ucallbacktypes.UniversalRead) bool { n++; return false }))
	require.Zero(t, n, "a look-alike log from another address must be ignored")
}

type accountArg struct {
	ChainNamespace string
	ChainId        string
	Owner          []byte
}

type readSpecArg struct {
	Account               accountArg
	Query                 []byte
	MinConfirmations      uint16
	BlockNumber           uint64
	ExpiryPushChainHeight uint64
	MaxFee                *big.Int
	RevertRecipient       common.Address
}

func loadRequestABI(t *testing.T) abi.ABI {
	t.Helper()
	raw, err := os.ReadFile("testdata/request_external_read_self.json")
	require.NoError(t, err)
	parsed, err := abi.JSON(strings.NewReader(string(raw)))
	require.NoError(t, err)
	return parsed
}
