package ante_test

import (
	"context"
	"fmt"
	"testing"

	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	clienttx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/pushchain/push-chain-node/app/ante"
	appparams "github.com/pushchain/push-chain-node/app/params"
	uexecutortypes "github.com/pushchain/push-chain-node/x/uexecutor/types"
)

// uexecutorModuleEVMAddr is the EVM address of the uexecutor module account -
// sha256("uexecutor")[:20]. The UEA contract trusts calls coming from it
// unconditionally, which is what makes aliasing onto it so damaging.
const uexecutorModuleEVMAddr = "0x14191Ea54B4c176fCf86f51b0FAc7CB1E71Df7d7"

const anteTestChainID = "push_9000-1"

// newSignerBindingEncodingConfig returns an encoding config able to build and
// sign real uexecutor transactions.
func newSignerBindingEncodingConfig(t *testing.T) appparams.EncodingConfig {
	t.Helper()
	encCfg := appparams.MakeEncodingConfig()
	std.RegisterInterfaces(encCfg.InterfaceRegistry)
	authtypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	uexecutortypes.RegisterInterfaces(encCfg.InterfaceRegistry)
	return encCfg
}

// aliasedSigner returns a `length`-byte address whose RIGHTMOST 20 bytes are the
// uexecutor module account. common.BytesToAddress keeps exactly those bytes, so
// every such address collapses onto the module's EVM address.
func aliasedSigner(t *testing.T, length int) sdk.AccAddress {
	t.Helper()
	require.Greater(t, length, common.AddressLength)

	moduleAddr := authtypes.NewModuleAddress(uexecutortypes.ModuleName)
	require.Len(t, moduleAddr, common.AddressLength)
	require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(moduleAddr).Hex())

	prefix := make([]byte, length-common.AddressLength)
	prefix[0] = 0x01
	addr := sdk.AccAddress(append(prefix, moduleAddr...))
	require.Len(t, addr, length)

	// The whole point of the finding: this longer address truncates onto the
	// module's EVM address downstream.
	require.Equal(t, uexecutorModuleEVMAddr, common.BytesToAddress(addr).Hex())
	return addr
}

// gaslessMsgFor builds one of the two user-facing gasless messages with the
// given declared signer.
func gaslessMsgFor(t *testing.T, msgType string, signer sdk.AccAddress) sdk.Msg {
	t.Helper()
	ua := &uexecutortypes.UniversalAccountId{
		ChainNamespace: "eip155",
		ChainId:        "11155111",
		Owner:          "0x000000000000000000000000000000000000dead",
	}

	switch msgType {
	case "MsgExecutePayload":
		return &uexecutortypes.MsgExecutePayload{
			Signer:             signer.String(),
			UniversalAccountId: ua,
			UniversalPayload: &uexecutortypes.UniversalPayload{
				To:   "0x000000000000000000000000000000000000dead",
				Data: "0xabcdef",
			},
			VerificationData: "0xabcdef",
		}
	case "MsgMigrateUEA":
		return &uexecutortypes.MsgMigrateUEA{
			Signer:             signer.String(),
			UniversalAccountId: ua,
			MigrationPayload: &uexecutortypes.MigrationPayload{
				Migration: "0x000000000000000000000000000000000000beef",
				Nonce:     "0",
				Deadline:  "1",
			},
			Signature: "0xabcdef",
		}
	default:
		t.Fatalf("unknown msg type %q", msgType)
		return nil
	}
}

// buildSignedTx returns a tx carrying msg whose declared signer is
// `declaredSigner` but which is signed by `priv` - the two need not be related,
// which is exactly the confusion the fix has to reject.
func buildSignedTx(t *testing.T, encCfg appparams.EncodingConfig, msg sdk.Msg, declaredSigner sdk.AccAddress, priv cryptotypes.PrivKey) sdk.Tx {
	t.Helper()

	txb := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, txb.SetMsgs(msg))
	txb.SetGasLimit(300_000)

	require.NoError(t, txb.SetSignatures(signing.SignatureV2{
		PubKey:   priv.PubKey(),
		Data:     &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT},
		Sequence: 0,
	}))

	// The gasless new-account path signs over account number 0 / sequence 0,
	// since the account does not exist on chain yet.
	signerData := authsigning.SignerData{
		Address:       declaredSigner.String(),
		ChainID:       anteTestChainID,
		AccountNumber: 0,
		Sequence:      0,
		PubKey:        priv.PubKey(),
	}

	sigV2, err := clienttx.SignWithPrivKey(
		context.Background(), signing.SignMode_SIGN_MODE_DIRECT, signerData,
		txb, priv, encCfg.TxConfig, 0,
	)
	require.NoError(t, err)
	require.NoError(t, txb.SetSignatures(sigV2))

	return txb.GetTx()
}

func newSignerBindingDecorator(t *testing.T, encCfg appparams.EncodingConfig) (ante.AccountInitDecorator, *mockAccountKeeperAnte) {
	t.Helper()
	ak := newMockAccountKeeperAnte(sdk.AccAddress([]byte("feeCollector")))
	return ante.NewAccountInitDecorator(ak, encCfg.TxConfig.SignModeHandler()), ak
}

// TestAccountInitDecorator_RejectsAliasedModuleSigner is the regression test for
// F-2026-18200: a gasless tx may not declare an over-long signer that truncates
// onto the uexecutor module address while being signed by an unrelated key.
//
// Hacken's PoC only used the 21-byte case; truncation works for ANY length > 20,
// so 21, 22 and 32 bytes are all covered, against both gasless messages.
func TestAccountInitDecorator_RejectsAliasedModuleSigner(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	for _, msgType := range []string{"MsgExecutePayload", "MsgMigrateUEA"} {
		for _, length := range []int{21, 22, 32} {
			t.Run(fmt.Sprintf("%s/%dbytes", msgType, length), func(t *testing.T) {
				attackerKey := secp256k1.GenPrivKey()
				declaredSigner := aliasedSigner(t, length)
				msg := gaslessMsgFor(t, msgType, declaredSigner)
				tx := buildSignedTx(t, encCfg, msg, declaredSigner, attackerKey)

				aid, ak := newSignerBindingDecorator(t, encCfg)
				ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

				nextCalled := false
				_, err := aid.AnteHandle(ctx, tx, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
					nextCalled = true
					return ctx, nil
				})

				require.Error(t, err, "aliased signer must not pass the ante chain")
				require.True(t, sdkerrors.ErrInvalidPubKey.Is(err), "expected ErrInvalidPubKey, got: %v", err)
				require.False(t, nextCalled, "the message must never reach execution")
				require.False(t, ak.HasAccount(context.Background(), declaredSigner),
					"no account may be persisted for a rejected signer")
			})
		}
	}
}

// TestAccountInitDecorator_RejectsMismatchedSigner covers the general case: a
// well-formed 20-byte signer that is simply not the address of the signing key.
func TestAccountInitDecorator_RejectsMismatchedSigner(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	attackerKey := secp256k1.GenPrivKey()
	victimKey := secp256k1.GenPrivKey()
	declaredSigner := sdk.AccAddress(victimKey.PubKey().Address())

	msg := gaslessMsgFor(t, "MsgExecutePayload", declaredSigner)
	tx := buildSignedTx(t, encCfg, msg, declaredSigner, attackerKey)

	aid, ak := newSignerBindingDecorator(t, encCfg)
	ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

	_, err := aid.AnteHandle(ctx, tx, false, emptyNext)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrInvalidPubKey.Is(err), "expected ErrInvalidPubKey, got: %v", err)
	require.False(t, ak.HasAccount(context.Background(), declaredSigner))
}

// TestAccountInitDecorator_AcceptsMatchingSigner is the positive control: a
// normal 20-byte signer whose key matches still creates the account and passes.
func TestAccountInitDecorator_AcceptsMatchingSigner(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	for _, msgType := range []string{"MsgExecutePayload", "MsgMigrateUEA"} {
		t.Run(msgType, func(t *testing.T) {
			key := secp256k1.GenPrivKey()
			signer := sdk.AccAddress(key.PubKey().Address())
			require.Len(t, signer, common.AddressLength)

			msg := gaslessMsgFor(t, msgType, signer)
			tx := buildSignedTx(t, encCfg, msg, signer, key)

			aid, ak := newSignerBindingDecorator(t, encCfg)
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			_, err := aid.AnteHandle(ctx, tx, false, emptyNext)
			require.NoError(t, err)

			acc := ak.GetAccount(context.Background(), signer)
			require.NotNil(t, acc, "the account must be created for a legitimate gasless tx")
			require.Equal(t, uint64(1), acc.GetSequence())
		})
	}
}

// TestAccountInitDecorator_SimulationUnaffected checks that the new binding
// check keeps the SDK's `!simulate` guard, so simulation and gas estimation -
// which carry no usable signature - keep working.
func TestAccountInitDecorator_SimulationUnaffected(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	attackerKey := secp256k1.GenPrivKey()
	victimKey := secp256k1.GenPrivKey()

	for name, declaredSigner := range map[string]sdk.AccAddress{
		"matching_signer":   sdk.AccAddress(attackerKey.PubKey().Address()),
		"mismatched_signer": sdk.AccAddress(victimKey.PubKey().Address()),
	} {
		t.Run(name, func(t *testing.T) {
			msg := gaslessMsgFor(t, "MsgExecutePayload", declaredSigner)
			tx := buildSignedTx(t, encCfg, msg, declaredSigner, attackerKey)

			aid, _ := newSignerBindingDecorator(t, encCfg)
			ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

			_, err := aid.AnteHandle(ctx, tx, true /* simulate */, emptyNext)
			require.NoError(t, err, "simulation must not be affected by the binding check")
		})
	}
}

// TestAccountInitDecorator_EnforcesSignatureLimit covers F-2026-18186: the
// new-account path short-circuits the ante chain, so it has to enforce the
// signature count limit itself instead of verifying an unbounded multisig for
// free.
func TestAccountInitDecorator_EnforcesSignatureLimit(t *testing.T) {
	encCfg := newSignerBindingEncodingConfig(t)

	params := authtypes.DefaultParams()
	numKeys := int(params.TxSigLimit) + 1

	pubKeys := make([]cryptotypes.PubKey, numKeys)
	sigs := make([]signing.SignatureData, numKeys)
	bitArray := cryptotypes.NewCompactBitArray(numKeys)
	for i := 0; i < numKeys; i++ {
		pubKeys[i] = secp256k1.GenPrivKey().PubKey()
		sigs[i] = &signing.SingleSignatureData{
			SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
			Signature: []byte("not-checked-the-limit-trips-first"),
		}
		bitArray.SetIndex(i, true)
	}

	multisigPk := kmultisig.NewLegacyAminoPubKey(numKeys, pubKeys)
	signer := sdk.AccAddress(multisigPk.Address())

	txb := encCfg.TxConfig.NewTxBuilder()
	require.NoError(t, txb.SetMsgs(gaslessMsgFor(t, "MsgExecutePayload", signer)))
	txb.SetGasLimit(300_000)
	require.NoError(t, txb.SetSignatures(signing.SignatureV2{
		PubKey:   multisigPk,
		Data:     &signing.MultiSignatureData{BitArray: bitArray, Signatures: sigs},
		Sequence: 0,
	}))

	aid, ak := newSignerBindingDecorator(t, encCfg)
	ctx := newAnteTestCtx(t, false).WithChainID(anteTestChainID)

	_, err := aid.AnteHandle(ctx, txb.GetTx(), false, emptyNext)
	require.Error(t, err)
	require.True(t, sdkerrors.ErrTooManySignatures.Is(err), "expected ErrTooManySignatures, got: %v", err)
	require.False(t, ak.HasAccount(context.Background(), signer))
}
