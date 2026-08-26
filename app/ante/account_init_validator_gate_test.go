package ante_test

import (
	"context"
	"fmt"
	"testing"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/ante"
	appparams "github.com/pushchain/push-chain-node/app/params"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
	utsstypes "github.com/pushchain/push-chain-node/x/utss/types"
)

// ---------------------------------------------------------------------------
// mock uvalidator keeper
// ---------------------------------------------------------------------------

// mockUValidatorKeeperAnte satisfies ante.UValidatorKeeper and mirrors the real
// keeper's return shape, which matters: x/uvalidator's
// IsBondedUniversalValidator returns an ERROR (not (false, nil)) for an address
// that is absent from the universal validator set, and (false, nil) only for a
// registered-but-unbonded one. Both have to be treated as a rejection.
type mockUValidatorKeeperAnte struct {
	// registered maps bech32 account address -> bonded.
	registered map[string]bool
}

func newMockUValidatorKeeperAnte(bonded ...sdk.AccAddress) *mockUValidatorKeeperAnte {
	m := &mockUValidatorKeeperAnte{registered: map[string]bool{}}
	for _, addr := range bonded {
		m.registered[addr.String()] = true
	}
	return m
}

// withUnbonded registers an address that is in the universal validator set but
// whose stake is not bonded - the (false, nil) branch of the real keeper.
func (m *mockUValidatorKeeperAnte) withUnbonded(addr sdk.AccAddress) *mockUValidatorKeeperAnte {
	m.registered[addr.String()] = false
	return m
}

func (m *mockUValidatorKeeperAnte) IsBondedUniversalValidator(_ context.Context, universalValidator string) (bool, error) {
	bonded, ok := m.registered[universalValidator]
	if !ok {
		return false, fmt.Errorf("validator %s not present in the registered universal validators set", universalValidator)
	}
	return bonded, nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// validatorOnlyMsgTypes are the five gasless message types that only a bonded
// universal validator can ever execute successfully.
var validatorOnlyMsgTypes = []string{
	"MsgVoteInbound",
	"MsgVoteOutbound",
	"MsgVoteChainMeta",
	"MsgVoteTssKeyProcess",
	"MsgVoteFundMigration",
}

// voteMsgFor builds one of the five validator-only gasless messages with the
// given declared signer.
func voteMsgFor(t *testing.T, msgType string, signer sdk.AccAddress) sdk.Msg {
	t.Helper()
	switch msgType {
	case "MsgVoteInbound":
		return &uexecutortypes.MsgVoteInbound{Signer: signer.String()}
	case "MsgVoteOutbound":
		return &uexecutortypes.MsgVoteOutbound{Signer: signer.String(), TxId: "0xdead", UtxId: "0xbeef"}
	case "MsgVoteChainMeta":
		return &uexecutortypes.MsgVoteChainMeta{
			Signer:          signer.String(),
			ObservedChainId: "eip155:11155111",
			Price:           1,
			ChainHeight:     2,
		}
	case "MsgVoteTssKeyProcess":
		return &utsstypes.MsgVoteTssKeyProcess{Signer: signer.String(), TssPubkey: "0xpub", KeyId: "key-1", ProcessId: 1}
	case "MsgVoteFundMigration":
		return &utsstypes.MsgVoteFundMigration{Signer: signer.String(), MigrationId: 1, TxHash: "0xdead", Success: true}
	default:
		t.Fatalf("unknown vote msg type %q", msgType)
		return nil
	}
}

// newGateDecorator builds the decorator under test with a uvalidator mock that
// knows only about `bondedUVs`.
func newGateDecorator(t *testing.T, encCfg appparams.EncodingConfig, uvk *mockUValidatorKeeperAnte) (ante.AccountInitDecorator, *mockAccountKeeperAnte) {
	t.Helper()
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	return ante.NewAccountInitDecorator(ak, uvk, encCfg.TxConfig.SignModeHandler()), ak
}

// ---------------------------------------------------------------------------
// F-2026-18186 - the finding itself
// ---------------------------------------------------------------------------

// TestAccountInitDecorator_VoteFromFreshSignerCreatesNoAccount is the regression
// test for F-2026-18186 (remediation 3).
//
// AccountInitDecorator writes the account row and then returns WITHOUT running
// the message, so the row survives even though the message subsequently fails.
// For the five validator-only vote messages that is a free, repeatable
// state-bloat primitive: a fresh key sends a gasless vote, the ante cache
// commits the account, and the msg server then rejects the vote because the
// signer is not a bonded universal validator.
//
// The load-bearing assertion is HasAccount == false; it is asserted BEFORE the
// error assertion on purpose, because require.Error aborts the subtest and would
// otherwise mask a vacuous pass.
func TestAccountInitDecorator_VoteFromFreshSignerCreatesNoAccount(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	for _, msgType := range validatorOnlyMsgTypes {
		t.Run(msgType, func(t *testing.T) {
			key := secp256k1.GenPrivKey()
			signer := sdk.AccAddress(key.PubKey().Address())

			// Correctly signed by its own key: the tx is valid in every respect
			// except that the signer is not a universal validator.
			tx := buildSignedTx(t, encCfg, voteMsgFor(t, msgType, signer), signer, key)

			// The uvalidator mock knows about nobody: this signer is a fresh key.
			aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte())
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			nextCalled := false
			_, err := aid.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				nextCalled = true
				return ctx, nil
			})

			// THE finding: no account row may be written for a message that
			// cannot succeed. Asserted first so a vacuous test cannot hide.
			require.False(t, ak.HasAccount(context.Background(), signer),
				"F-2026-18186: no account row may be persisted for a gasless vote from a non-validator signer")

			require.Error(t, err, "a gasless vote from a non-validator signer must be rejected")
			require.True(t, sdkerrors.ErrUnauthorized.Is(err), "expected ErrUnauthorized, got: %v", err)
			require.False(t, nextCalled, "the message must never reach execution")
		})
	}
}

// TestAccountInitDecorator_VoteFromRegisteredButUnbondedSigner covers the other
// rejection branch of the real keeper: an address that IS in the universal
// validator set but whose stake is not bonded returns (false, nil) rather than
// an error, and must be rejected just the same.
func TestAccountInitDecorator_VoteFromRegisteredButUnbondedSigner(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	key := secp256k1.GenPrivKey()
	signer := sdk.AccAddress(key.PubKey().Address())
	tx := buildSignedTx(t, encCfg, voteMsgFor(t, "MsgVoteInbound", signer), signer, key)

	aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte().withUnbonded(signer))
	ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

	_, err := aid.AnteHandle(ctx, tx, false, emptyNext)

	require.False(t, ak.HasAccount(context.Background(), signer),
		"a registered-but-unbonded signer must not get an account row either")
	require.Error(t, err)
	require.True(t, sdkerrors.ErrUnauthorized.Is(err), "expected ErrUnauthorized, got: %v", err)
	require.Contains(t, err.Error(), "not a bonded universal validator")
}

// TestAccountInitDecorator_GateRunsBeforeSignatureVerification pins the ordering.
// The gate is meant to reject before the expensive signature verification, so a
// vote tx that is BOTH signed by an unrelated key AND sent from a non-validator
// signer must come back with the validator rejection, not ErrInvalidPubKey.
func TestAccountInitDecorator_GateRunsBeforeSignatureVerification(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	attackerKey := secp256k1.GenPrivKey()
	victimKey := secp256k1.GenPrivKey()
	declaredSigner := sdk.AccAddress(victimKey.PubKey().Address())

	tx := buildSignedTx(t, encCfg, voteMsgFor(t, "MsgVoteInbound", declaredSigner), declaredSigner, attackerKey)

	aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte())
	ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

	_, err := aid.AnteHandle(ctx, tx, false, emptyNext)

	require.False(t, ak.HasAccount(context.Background(), declaredSigner))
	require.Error(t, err)
	require.True(t, sdkerrors.ErrUnauthorized.Is(err),
		"the validator gate must fire before signature verification, got: %v", err)
	require.False(t, sdkerrors.ErrInvalidPubKey.Is(err))
}

// ---------------------------------------------------------------------------
// no regression: the legitimate paths
// ---------------------------------------------------------------------------

// TestAccountInitDecorator_BondedValidatorVoteStillWorks is the positive control
// for the gate: a bonded universal validator's vote passes it, for all five
// message types.
//
// Both sub-cases matter. In practice a bonded universal validator already has an
// account, so it takes the "existing account" branch and reaches next(); the
// no-account variant proves the gate itself is not what would reject it if it
// somehow did not.
func TestAccountInitDecorator_BondedValidatorVoteStillWorks(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	for _, msgType := range validatorOnlyMsgTypes {
		t.Run(msgType+"/no_account_yet", func(t *testing.T) {
			key := secp256k1.GenPrivKey()
			signer := sdk.AccAddress(key.PubKey().Address())
			tx := buildSignedTx(t, encCfg, voteMsgFor(t, msgType, signer), signer, key)

			aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte(signer))
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			_, err := aid.AnteHandle(ctx, tx, false, emptyNext)
			require.NoError(t, err, "a bonded universal validator must not be rejected by the gate")

			acc := ak.GetAccount(context.Background(), signer)
			require.NotNil(t, acc, "the bonded validator's account is still created")
			require.Equal(t, uint64(1), acc.GetSequence())
		})

		t.Run(msgType+"/existing_account", func(t *testing.T) {
			key := secp256k1.GenPrivKey()
			signer := sdk.AccAddress(key.PubKey().Address())
			tx := buildSignedTx(t, encCfg, voteMsgFor(t, msgType, signer), signer, key)

			aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte(signer))
			ak.SetAccount(context.Background(), authtypes.NewBaseAccountWithAddress(signer))
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			nextCalled := false
			_, err := aid.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
				nextCalled = true
				return ctx, nil
			})
			require.NoError(t, err)
			require.True(t, nextCalled, "an existing account must still fall through to the rest of the ante chain")
		})
	}
}

// TestAccountInitDecorator_PermissionlessGaslessMsgsUngated proves the scoping.
// MsgExecutePayload and MsgMigrateUEA are permissionless by design: a first-time
// universal user has no account and no validator status, and creating the account
// for them is the intended behaviour of this decorator. Gating them would break
// real users, so they must still work against a uvalidator keeper that rejects
// every address.
func TestAccountInitDecorator_PermissionlessGaslessMsgsUngated(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	for _, msgType := range []string{"MsgExecutePayload", "MsgMigrateUEA"} {
		t.Run(msgType, func(t *testing.T) {
			key := secp256k1.GenPrivKey()
			signer := sdk.AccAddress(key.PubKey().Address())
			tx := buildSignedTx(t, encCfg, gaslessMsgFor(t, msgType, signer), signer, key)

			// Knows about nobody: it would reject the signer if it were consulted.
			aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte())
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			_, err := aid.AnteHandle(ctx, tx, false, emptyNext)
			require.NoError(t, err, "%s is permissionless and must not be gated on validator status", msgType)

			acc := ak.GetAccount(context.Background(), signer)
			require.NotNil(t, acc, "a first-time universal user must still get an account")
			require.Equal(t, uint64(1), acc.GetSequence())
		})
	}
}

// TestAccountInitDecorator_AuthzWrappedVoteUngated pins the deliberate decision
// not to unwrap authz.MsgExec.
//
// A universal validator submits its votes wrapped in authz.MsgExec
// (universalClient/pushsigner wrapWithAuthZ). There the TX signer is the grantee
// hotkey while the vote's own signer - the address the msg server checks - is the
// granter. The hotkey is legitimately not a universal validator, so unwrapping
// here would reject the real voting path.
func TestAccountInitDecorator_AuthzWrappedVoteUngated(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	hotKey := secp256k1.GenPrivKey()
	grantee := sdk.AccAddress(hotKey.PubKey().Address())
	granter := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())

	inner, err := codectypes.NewAnyWithValue(voteMsgFor(t, "MsgVoteInbound", granter))
	require.NoError(t, err)
	execMsg := &authz.MsgExec{Grantee: grantee.String(), Msgs: []*codectypes.Any{inner}}

	tx := buildSignedTx(t, encCfg, execMsg, grantee, hotKey)

	// Neither the hotkey nor the granter is known to the mock; only the absence of
	// the gate can let this through.
	aid, ak := newGateDecorator(t, encCfg, newMockUValidatorKeeperAnte())
	ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

	_, err = aid.AnteHandle(ctx, tx, false, emptyNext)
	require.NoError(t, err, "the authz-wrapped voting path must keep working for a fresh grantee hotkey")

	acc := ak.GetAccount(context.Background(), grantee)
	require.NotNil(t, acc, "the grantee hotkey must still get its account created")
	require.Equal(t, uint64(1), acc.GetSequence())
}
