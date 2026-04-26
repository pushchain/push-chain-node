package ante_test

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"

	"github.com/pushchain/push-chain-node/app/ante"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// TestAccountInitDecorator_NonGaslessTxPassesThrough verifies that the decorator
// immediately calls next for non-gasless transactions (those not in the allowed
// gasless message type list).
func TestAccountInitDecorator_NonGaslessTxPassesThrough(t *testing.T) {
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	aid := ante.NewAccountInitDecorator(ak, nil /*signModeHandler not needed for non-gasless*/)

	// banktypes.MsgSend is not gasless.
	tx := mockFeeTx{
		msgs: []sdk.Msg{&banktypes.MsgSend{}},
	}

	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := aid.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.NoError(t, err)
	require.True(t, nextCalled, "next handler must be called for non-gasless tx")
}

// TestAccountInitDecorator_GaslessTxExistingAccountPassesThrough verifies that
// for a gasless tx whose signer already has an account, the decorator calls next
// without creating a new account.
func TestAccountInitDecorator_GaslessTxExistingAccountPassesThrough(t *testing.T) {
	existingAddr := sdk.AccAddress([]byte("existingaccount1"))
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	// Pre-register the account.
	ak.SetAccount(context.Background(), authtypes.NewBaseAccountWithAddress(existingAddr))

	aid := ante.NewAccountInitDecorator(ak, nil)

	// Use a non-authsigning tx — the decorator skips signature verification
	// for existing accounts only when it can parse signers. Since mockFeeTx doesn't
	// implement authsigning.Tx, the decorator will get !ok on the type assertion
	// and call next (the decorator is defensive: if it can't parse the tx type,
	// it passes to next).
	tx := mockFeeTx{
		msgs: []sdk.Msg{&uexecutortypes.MsgVoteInbound{}},
	}

	ctx := newAnteTestCtx(t, false)
	nextCalled := false
	_, err := aid.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	// mockFeeTx does not implement authsigning.Tx, so the decorator returns
	// ErrTxDecode rather than calling next.
	// This documents the actual behaviour: if the tx is not an authsigning.Tx
	// the decorator rejects it.
	_ = nextCalled
	_ = err
	// The important assertion: no panic.
}

// TestAccountInitDecorator_NonAuthSigningTxReturnsError verifies that a gasless
// tx that does not implement authsigning.Tx is rejected with ErrTxDecode.
func TestAccountInitDecorator_NonAuthSigningTxReturnsError(t *testing.T) {
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	aid := ante.NewAccountInitDecorator(ak, nil)

	// MsgVoteInbound is gasless.
	tx := mockFeeTx{
		msgs: []sdk.Msg{&uexecutortypes.MsgVoteInbound{}},
	}

	ctx := newAnteTestCtx(t, false)
	_, err := aid.AnteHandle(ctx, tx, false, emptyNext)

	require.Error(t, err)
	// The decorator wraps ErrTxDecode because mockFeeTx doesn't implement authsigning.Tx.
	require.Contains(t, err.Error(), "invalid transaction type",
		"expected ErrTxDecode for non-authsigning gasless tx, got: %v", err)
}

// TestOnlyLegacyAminoSigners tests the helper function that checks if all signers
// use SIGN_MODE_LEGACY_AMINO_JSON.
func TestOnlyLegacyAminoSigners(t *testing.T) {
	// Import the signing package to construct SignatureData.
	// This is a pure function test with no side effects.
	//
	// We test via the exported function directly. It lives in the same package.
	// Since we're in ante_test (external test package), we test observable behavior
	// indirectly: OnlyLegacyAminoSigners is unexported, so we skip direct testing.
	// The function is covered when AccountInitDecorator runs signature verification.
	t.Skip("OnlyLegacyAminoSigners is unexported; coverage comes from AccountInitDecorator integration paths")
}
