package integrationtest

import (
	"math/big"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	utils "github.com/pushchain/push-chain-node/test/utils"
	ucallbacktypes "github.com/pushchain/push-chain-node/x/ucallback/types"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// A read requested from inside a UEA payload must be recorded.
//
// Payloads run through DerivedEVMCall, which never reaches ApplyTransaction and so
// never fires the EVM post-tx hook x/ucallback ingests from. Before the explicit
// hand-off in CallUEAExecutePayload the request was emitted and its budget
// escrowed with nothing recording it, leaving the funds unreachable: the sweeper
// only walks PendingByExpiry, which an un-ingested read never enters.
func TestReadRequestedInUEAPayload_IsIngested(t *testing.T) {
	chainApp, ctx, _ := utils.SetAppWithValidators(t)
	// The fixture leaves block time at the zero value, which reaches the EVM as a
	// huge unsigned timestamp and trips the payload's ExpiredDeadline check.
	ctx = ctx.WithBlockTime(time.Unix(1_700_000_000, 0))
	uek := chainApp.UexecutorKeeper
	uck := chainApp.UcallbackKeeper

	contract := utils.SetupUniversalCallback(t, chainApp, ctx)
	core := utils.SetupMockUniversalCoreForReads(t, chainApp, ctx)

	// _universalCore is storage slot 0; the fixture writes runtime code directly,
	// so initialize() never ran to set it.
	chainApp.EVMKeeper.SetState(ctx, contract,
		common.BigToHash(big.NewInt(0)), common.BytesToHash(core.Bytes()).Bytes())

	moduleAddr, _ := uek.GetUeModuleAddress(ctx)

	// A real UEA, deployed the way an inbound would deploy it.
	owner := utils.GetDefaultAddresses().DefaultTestAddr
	deployRes, err := uek.DeployUEAV2(ctx, moduleAddr, &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          owner,
	})
	require.NoError(t, err)
	uea := common.BytesToAddress(deployRes.Ret)
	require.NotEqual(t, common.Address{}, uea, "UEA must have been deployed")

	// The UEA pays the deposit out of its own balance.
	deposit := big.NewInt(4_000_000_000_000_000)
	fund(t, chainApp, ctx, sdk.AccAddress(uea.Bytes()), new(big.Int).Mul(deposit, big.NewInt(10)))

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

	payload := &uexecutortypes.UniversalPayload{
		To:                   contract.Hex(),
		Value:                deposit.String(),
		Data:                 common.Bytes2Hex(callData),
		GasLimit:             "21000000",
		MaxFeePerGas:         "1000000000",
		MaxPriorityFeePerGas: "200000000",
		Nonce:                "0",
		Deadline:             "9999999999",
		VType:                uexecutortypes.VerificationType(1),
	}

	res, err := uek.CallUEAExecutePayload(ctx, moduleAddr, uea, payload, nil)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Empty(t, res.VmError, "payload execution must not revert: %s", res.VmError)
	require.NotEmpty(t, res.Logs, "the contract must have emitted ReadRequested")

	var recorded []ucallbacktypes.UniversalRead
	require.NoError(t, uck.IterateReadsByTxHash(ctx, res.Hash,
		func(ur ucallbacktypes.UniversalRead) bool {
			recorded = append(recorded, ur)
			return false
		}))

	require.Len(t, recorded, 1,
		"the read must be recorded; without the hand-off in CallUEAExecutePayload "+
			"the hook never fires for a derived call and the escrowed budget is stranded")
	require.Equal(t, uint64(250_000), recorded[0].Request.CallbackGasLimit)
}
