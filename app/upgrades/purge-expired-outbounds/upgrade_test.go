package purgeexpiredoutbounds

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdkaddress "github.com/cosmos/cosmos-sdk/codec/address"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/testutil/integration"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	distrkeeper "github.com/cosmos/cosmos-sdk/x/distribution/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	slashingkeeper "github.com/cosmos/cosmos-sdk/x/slashing/keeper"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	feemarketkeeper "github.com/cosmos/evm/x/feemarket/keeper"

	"github.com/pushchain/push-chain-node/app/upgrades"
	uexecutorkeeper "github.com/pushchain/push-chain-node/x/uexecutor/keeper"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	uvalidatorkeeper "github.com/pushchain/push-chain-node/x/uvalidator/keeper"
	uvalidatortypes "github.com/pushchain/push-chain-node/x/uvalidator/types"
	"github.com/stretchr/testify/require"
)

const (
	bech32Prefix     = "push"
	bech32AccAddr    = bech32Prefix
	bech32AccPub     = bech32Prefix + sdk.PrefixPublic
	bech32ValAddr    = bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator
	bech32ValPub     = bech32Prefix + sdk.PrefixValidator + sdk.PrefixOperator + sdk.PrefixPublic
	bech32ConsAddr   = bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus
	bech32ConsPub    = bech32Prefix + sdk.PrefixValidator + sdk.PrefixConsensus + sdk.PrefixPublic
)

// setupKeepers creates minimal uexecutor and uvalidator keepers backed by real stores.
func setupKeepers(t *testing.T) (sdk.Context, *uexecutorkeeper.Keeper, *uvalidatorkeeper.Keeper) {
	t.Helper()

	cfg := sdk.GetConfig()
	cfg.SetBech32PrefixForAccount(bech32AccAddr, bech32AccPub)
	cfg.SetBech32PrefixForValidator(bech32ValAddr, bech32ValPub)
	cfg.SetBech32PrefixForConsensusNode(bech32ConsAddr, bech32ConsPub)

	logger := log.NewTestLogger(t)
	encCfg := moduletestutil.MakeTestEncodingConfig()

	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	stakingtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	banktypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	uexecutortypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	uvalidatortypes.RegisterInterfaces(encCfg.InterfaceRegistry)

	govModAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()

	accountAddressCodec := sdkaddress.NewBech32Codec(bech32AccAddr)
	validatorAddressCodec := sdkaddress.NewBech32Codec(bech32ValAddr)
	consensusAddressCodec := sdkaddress.NewBech32Codec(bech32ConsAddr)

	keys := storetypes.NewKVStoreKeys(
		authtypes.ModuleName,
		banktypes.ModuleName,
		stakingtypes.ModuleName,
		uexecutortypes.ModuleName,
		uvalidatortypes.ModuleName,
	)

	ctx := sdk.NewContext(integration.CreateMultiStore(keys, logger), cmtproto.Header{Height: 100}, false, logger)

	accountKeeper := authkeeper.NewAccountKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[authtypes.StoreKey]),
		authtypes.ProtoBaseAccount,
		map[string][]string{
			authtypes.FeeCollectorName:     nil,
			stakingtypes.BondedPoolName:    {authtypes.Burner, authtypes.Staking},
			stakingtypes.NotBondedPoolName: {authtypes.Burner, authtypes.Staking},
			govtypes.ModuleName:            {authtypes.Burner},
		},
		accountAddressCodec, bech32AccAddr,
		govModAddr,
	)

	bankKeeper := bankkeeper.NewBaseKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[banktypes.StoreKey]),
		accountKeeper, nil, govModAddr, logger,
	)

	stakingKeeper := stakingkeeper.NewKeeper(
		encCfg.Codec, runtime.NewKVStoreService(keys[stakingtypes.StoreKey]),
		accountKeeper, bankKeeper, govModAddr,
		validatorAddressCodec, consensusAddressCodec,
	)

	vk := uvalidatorkeeper.NewKeeper(
		encCfg.Codec,
		runtime.NewKVStoreService(keys[uvalidatortypes.ModuleName]),
		logger, govModAddr,
		bankKeeper, accountKeeper,
		distrkeeper.Keeper{}, stakingKeeper,
		slashingkeeper.Keeper{}, mockUtssKeeper{},
	)

	ek := uexecutorkeeper.NewKeeper(
		encCfg.Codec,
		runtime.NewKVStoreService(keys[uexecutortypes.ModuleName]),
		logger, govModAddr,
		nil, // evmKeeper
		&feemarketkeeper.Keeper{},
		nil, // bankKeeper
		authkeeper.AccountKeeper{},
		nil, // uregistryKeeper
		nil, // utxverifierKeeper
		&vk,
	)

	return ctx, &ek, &vk
}

type mockUtssKeeper struct{}

func (m mockUtssKeeper) GetCurrentTssParticipants(_ context.Context) ([]string, error) {
	return []string{"val1"}, nil
}
func (m mockUtssKeeper) HasOngoingTss(_ context.Context) (bool, error) {
	return false, nil
}

// seedPendingOutbound creates a UTX with a PENDING outbound and adds it to PendingOutbounds.
func seedPendingOutbound(t *testing.T, ctx sdk.Context, ek *uexecutorkeeper.Keeper, utxId, outboundId string, txType uexecutortypes.TxType) {
	t.Helper()

	utx := uexecutortypes.UniversalTx{
		OutboundTx: []*uexecutortypes.OutboundTx{
			{
				Id:               outboundId,
				OutboundStatus:   uexecutortypes.Status_PENDING,
				TxType:           txType,
				Prc20AssetAddr:   "0x387b9C8Db60E74999aAAC5A2b7825b400F12d68E",
				Amount:           "1000000",
				Sender:           "0x1234567890abcdef1234567890abcdef12345678",
				DestinationChain: "eip155:11155111",
			},
		},
	}
	require.NoError(t, ek.UniversalTx.Set(ctx, utxId, utx))
	require.NoError(t, ek.PendingOutbounds.Set(ctx, outboundId, uexecutortypes.PendingOutboundEntry{
		OutboundId:    outboundId,
		UniversalTxId: utxId,
		CreatedAt:     ctx.BlockHeight(),
	}))
}

// seedExpiredBallot creates a ballot with EXPIRED status for the given outbound observation.
func seedExpiredBallot(t *testing.T, ctx sdk.Context, vk *uvalidatorkeeper.Keeper, utxId, outboundId string, obs uexecutortypes.OutboundObservation) string {
	t.Helper()

	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(t, err)

	ballot, err := vk.CreateBallot(ctx, ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		[]string{"val1"}, 1, 5)
	require.NoError(t, err)

	ballot.Status = uvalidatortypes.BallotStatus_BALLOT_STATUS_EXPIRED
	require.NoError(t, vk.SetBallot(ctx, ballot))

	// Move to expired set (mimic MarkBallotExpired)
	_ = vk.ActiveBallotIDs.Remove(ctx, ballotKey)
	require.NoError(t, vk.ExpiredBallotIDs.Set(ctx, ballotKey))

	return ballotKey
}

func TestDeleteExpiredOutboundBallots_DeletesExpiredBallots(t *testing.T) {
	ctx, ek, vk := setupKeepers(t)
	require := require.New(t)

	obs := uexecutortypes.OutboundObservation{
		Success:    false,
		ErrorMsg:   "event expired before TSS could start",
		GasFeeUsed: "0",
	}

	// Seed 3 pending outbounds with expired ballots
	seedPendingOutbound(t, ctx, ek, "utx-1", "ob-1", uexecutortypes.TxType_FUNDS)
	ballotKey1 := seedExpiredBallot(t, ctx, vk, "utx-1", "ob-1", obs)

	seedPendingOutbound(t, ctx, ek, "utx-2", "ob-2", uexecutortypes.TxType_FUNDS_AND_PAYLOAD)
	ballotKey2 := seedExpiredBallot(t, ctx, vk, "utx-2", "ob-2", obs)

	seedPendingOutbound(t, ctx, ek, "utx-3", "ob-3", uexecutortypes.TxType_GAS_AND_PAYLOAD)
	ballotKey3 := seedExpiredBallot(t, ctx, vk, "utx-3", "ob-3", obs)

	// Run the upgrade logic
	ak := &upgrades.AppKeepers{
		UExecutorKeeper:  ek,
		UValidatorKeeper: vk,
	}
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, ak)

	require.Equal(3, deleted, "should delete 3 expired ballots")
	require.Equal(0, skipped)
	require.Equal(0, errCount)

	// Verify ballots are deleted
	_, err := vk.Ballots.Get(ctx, ballotKey1)
	require.Error(err, "ballot 1 should be deleted")

	_, err = vk.Ballots.Get(ctx, ballotKey2)
	require.Error(err, "ballot 2 should be deleted")

	_, err = vk.Ballots.Get(ctx, ballotKey3)
	require.Error(err, "ballot 3 should be deleted")

	// Verify expired ballot IDs are cleaned up
	has, _ := vk.ExpiredBallotIDs.Has(ctx, ballotKey1)
	require.False(has, "ballot 1 should be removed from expired set")

	// Pending outbounds should still exist (only ballots are deleted, not outbounds)
	has, err = ek.PendingOutbounds.Has(ctx, "ob-1")
	require.NoError(err)
	require.True(has, "pending outbound should still exist for re-voting")
}

func TestDeleteExpiredOutboundBallots_SkipsActiveBallots(t *testing.T) {
	ctx, ek, vk := setupKeepers(t)
	require := require.New(t)

	obs := uexecutortypes.OutboundObservation{
		Success:    false,
		ErrorMsg:   "event expired before TSS could start",
		GasFeeUsed: "0",
	}

	// Seed one expired and one active ballot
	seedPendingOutbound(t, ctx, ek, "utx-expired", "ob-expired", uexecutortypes.TxType_FUNDS)
	expiredKey := seedExpiredBallot(t, ctx, vk, "utx-expired", "ob-expired", obs)

	seedPendingOutbound(t, ctx, ek, "utx-active", "ob-active", uexecutortypes.TxType_FUNDS)
	activeKey, err := uexecutortypes.GetOutboundBallotKey("utx-active", "ob-active", obs)
	require.NoError(err)
	_, err = vk.CreateBallot(ctx, activeKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		[]string{"val1"}, 1, 100000000)
	require.NoError(err)

	// Run the upgrade logic
	ak := &upgrades.AppKeepers{
		UExecutorKeeper:  ek,
		UValidatorKeeper: vk,
	}
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, ak)

	require.Equal(1, deleted, "should only delete the expired ballot")
	require.Equal(1, skipped, "should skip the active ballot")
	require.Equal(0, errCount)

	// Expired ballot should be gone
	_, err = vk.Ballots.Get(ctx, expiredKey)
	require.Error(err)

	// Active ballot should still exist
	ballot, err := vk.Ballots.Get(ctx, activeKey)
	require.NoError(err)
	require.Equal(uvalidatortypes.BallotStatus_BALLOT_STATUS_PENDING, ballot.Status)
}

func TestDeleteExpiredOutboundBallots_NoPendingOutbounds(t *testing.T) {
	ctx, ek, vk := setupKeepers(t)
	require := require.New(t)

	ak := &upgrades.AppKeepers{
		UExecutorKeeper:  ek,
		UValidatorKeeper: vk,
	}
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, ak)

	require.Equal(0, deleted)
	require.Equal(0, skipped)
	require.Equal(0, errCount)
}

func TestDeleteExpiredOutboundBallots_OrphanedPendingOutbound(t *testing.T) {
	ctx, ek, vk := setupKeepers(t)
	require := require.New(t)

	// Pending outbound exists but no ballot was ever created
	seedPendingOutbound(t, ctx, ek, "utx-orphan", "ob-orphan", uexecutortypes.TxType_FUNDS)

	ak := &upgrades.AppKeepers{
		UExecutorKeeper:  ek,
		UValidatorKeeper: vk,
	}
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, ak)

	// No ballot found at all → findExpiredBallot returns false → skipped
	require.Equal(0, deleted)
	require.Equal(1, skipped)
	require.Equal(0, errCount)
}

func TestFindExpiredBallot_MatchesVariousObservations(t *testing.T) {
	ctx, _, vk := setupKeepers(t)
	require := require.New(t)

	utxId := "utx-test"
	outboundId := "ob-test"

	// Create expired ballot with success=false, no error msg
	obs := uexecutortypes.OutboundObservation{Success: false}
	ballotKey := seedExpiredBallot(t, ctx, vk, utxId, outboundId, obs)

	foundKey, found := findExpiredBallot(ctx, vk, utxId, outboundId)
	require.True(found)
	require.Equal(ballotKey, foundKey)
}

func TestFindExpiredBallot_ReturnsFalseForPendingBallot(t *testing.T) {
	ctx, _, vk := setupKeepers(t)
	require := require.New(t)

	utxId := "utx-pending"
	outboundId := "ob-pending"

	obs := uexecutortypes.OutboundObservation{
		Success:    false,
		ErrorMsg:   "event expired before TSS could start",
		GasFeeUsed: "0",
	}
	ballotKey, err := uexecutortypes.GetOutboundBallotKey(utxId, outboundId, obs)
	require.NoError(err)

	_, err = vk.CreateBallot(ctx, ballotKey,
		uvalidatortypes.BallotObservationType_BALLOT_OBSERVATION_TYPE_OUTBOUND_TX,
		[]string{"val1"}, 1, 100000000)
	require.NoError(err)

	_, found := findExpiredBallot(ctx, vk, utxId, outboundId)
	require.False(found, "should not find expired ballot when ballot is PENDING")
}

func TestDeleteExpiredOutboundBallots_NilKeepers(t *testing.T) {
	ctx, _, _ := setupKeepers(t)
	require := require.New(t)

	// Nil AppKeepers
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, nil)
	require.Equal(0, deleted)
	require.Equal(0, skipped)
	require.Equal(1, errCount)

	// Nil UExecutorKeeper
	deleted, skipped, errCount = deleteExpiredOutboundBallots(ctx, &upgrades.AppKeepers{
		UExecutorKeeper:  nil,
		UValidatorKeeper: nil,
	})
	require.Equal(0, deleted)
	require.Equal(0, skipped)
	require.Equal(1, errCount)
}

func TestSafeDeleteExpiredOutboundBallots_RecoverFromPanic(t *testing.T) {
	ctx, _, _ := setupKeepers(t)
	require := require.New(t)

	// Pass nil keepers — safeDeleteExpiredOutboundBallots should not panic
	require.NotPanics(func() {
		deleted, skipped, errCount := safeDeleteExpiredOutboundBallots(ctx, nil)
		require.Equal(0, deleted)
		require.Equal(0, skipped)
		require.Equal(1, errCount)
	})
}

func TestDeleteExpiredOutboundBallots_EmptyEntryIDs(t *testing.T) {
	ctx, ek, vk := setupKeepers(t)
	require := require.New(t)

	// Seed a pending outbound entry with empty universalTxId
	require.NoError(ek.PendingOutbounds.Set(ctx, "ob-bad", uexecutortypes.PendingOutboundEntry{
		OutboundId:    "ob-bad",
		UniversalTxId: "",
		CreatedAt:     ctx.BlockHeight(),
	}))

	ak := &upgrades.AppKeepers{
		UExecutorKeeper:  ek,
		UValidatorKeeper: vk,
	}
	deleted, skipped, errCount := deleteExpiredOutboundBallots(ctx, ak)

	require.Equal(0, deleted)
	require.Equal(0, skipped)
	require.Equal(1, errCount, "should count empty ID as error")
}
