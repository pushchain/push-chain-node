package ante

import (
	"bytes"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"google.golang.org/protobuf/types/known/anypb"

	errorsmod "cosmossdk.io/errors"
	storetypes "cosmossdk.io/store/types"
	txsigning "cosmossdk.io/x/tx/signing"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	txpolicy "github.com/pushchain/push-chain-node/app/txpolicy"
)

type AccountInitDecorator struct {
	ak              AccountKeeper
	signModeHandler *txsigning.HandlerMap
	sigGasConsumer  SignatureVerificationGasConsumer
}

// SignatureVerificationGasConsumer charges gas for a single signature, matching
// the ante.SignatureVerificationGasConsumer contract used by the SDK's
// SigGasConsumeDecorator.
type SignatureVerificationGasConsumer func(meter storetypes.GasMeter, sig signing.SignatureV2, params authtypes.Params) error

func NewAccountInitDecorator(ak AccountKeeper, signModeHandler *txsigning.HandlerMap, sigGasConsumer SignatureVerificationGasConsumer) AccountInitDecorator {
	if sigGasConsumer == nil {
		sigGasConsumer = ante.DefaultSigVerificationGasConsumer
	}

	return AccountInitDecorator{
		ak:              ak,
		signModeHandler: signModeHandler,
		sigGasConsumer:  sigGasConsumer,
	}
}

func (aid AccountInitDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if !txpolicy.IsGaslessTx(tx) {
		// Skip account initialization for non-gasless transactions
		ctx.Logger().Debug("account init decorator: non-gasless tx, skipping account init")
		return next(ctx, tx, simulate)
	}

	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return ctx, errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}

	signers, err := sigTx.GetSigners()
	if err != nil || len(signers) != 1 {
		ctx.Logger().Debug("account init decorator: could not get unique signer, passing to next handler",
			"num_signers", len(signers),
			"error", err,
		)
		return next(ctx, tx, simulate)
	}

	newAccAddr := signers[0]
	if !aid.ak.HasAccount(ctx, newAccAddr) {
		ctx.Logger().Debug("account init decorator: new account detected on gasless tx, verifying signature",
			"address", sdk.AccAddress(newAccAddr).String(),
			"simulate", simulate,
		)
		// if account does not exist on chain, bypass rest of ante chain here.
		// Perform signature verification on account number e and sequence number e instead.
		if err := aid.verifySignatureForNewAccount(ctx, tx, simulate); err != nil {
			ctx.Logger().Debug("account init decorator: signature verification failed for new account",
				"address", sdk.AccAddress(newAccAddr).String(),
				"error", err,
			)
			return ctx, err
		}

		acc := aid.ak.NewAccountWithAddress(ctx, newAccAddr)
		acc.SetSequence(1)
		aid.ak.SetAccount(ctx, acc)
		ctx.Logger().Info("account init decorator: new account created via gasless tx",
			"address", sdk.AccAddress(newAccAddr).String(),
		)
		return ctx, nil
	}

	ctx.Logger().Debug("account init decorator: existing account on gasless tx, passing to next handler",
		"address", sdk.AccAddress(newAccAddr).String(),
	)
	return next(ctx, tx, simulate)
}

func (aid AccountInitDecorator) verifySignatureForNewAccount(ctx sdk.Context, tx sdk.Tx, simulate bool) error {
	sigTx, ok := tx.(authsigning.Tx)
	if !ok {
		return errorsmod.Wrap(sdkerrors.ErrTxDecode, "invalid transaction type")
	}

	// stdSigs contains the sequence number, account number, and signatures.
	// When simulating, this would just be a 0-length slice.
	sigs, err := sigTx.GetSignaturesV2()
	if err != nil {
		return err
	}

	signers, err := sigTx.GetSigners()
	if err != nil {
		return err
	}

	// check that signer length and signature length are the same
	if len(sigs) != len(signers) {
		return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid number of signer;  expected: %d, got %d", len(signers), len(sigs))
	}

	params := aid.ak.GetParams(ctx)

	// Enforce the signature count limit before doing any verification work.
	// This decorator short-circuits the ante chain for new accounts, so
	// ante.ValidateSigCountDecorator never runs for them; without this an
	// unpriced gasless tx could carry an arbitrarily large multisig key and
	// force the node to verify every sub-signature for free.
	sigCount := 0
	for _, sig := range sigs {
		if sig.PubKey == nil {
			return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey is not provided in signature")
		}
		sigCount += ante.CountSubKeys(sig.PubKey)
		if uint64(sigCount) > params.TxSigLimit {
			return errorsmod.Wrapf(sdkerrors.ErrTooManySignatures,
				"signatures: %d, limit: %d", sigCount, params.TxSigLimit)
		}
	}

	newAccAddr := sdk.AccAddress(signers[0])
	for i, sig := range sigs {
		pubKey := sig.PubKey
		if pubKey == nil {
			return errorsmod.Wrap(sdkerrors.ErrInvalidPubKey, "pubkey is not provided in signature")
		}

		// Bind the declared signer to the key that actually signed the tx.
		//
		// VerifySignature below only proves "this key signed this tx"; it says
		// nothing about WHO the tx claims to be from. Because this decorator
		// short-circuits the ante chain for new accounts, the SDK's
		// SetPubKeyDecorator - which owns this check - never runs, so a tx could
		// declare an arbitrary signer while being signed by an unrelated key.
		// Bech32 account addresses may be up to 255 bytes, and downstream
		// conversion to a 20-byte EVM address keeps only the rightmost bytes, so
		// a crafted longer signer could alias a module address.
		//
		// Guards mirror x/auth/ante/sigverify.go exactly so simulation and gas
		// estimation keep working.
		if !simulate && ctx.IsSigverifyTx() && !bytes.Equal(pubKey.Address().Bytes(), signers[i]) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidPubKey,
				"pubKey does not match signer address %s with signer index: %d", sdk.AccAddress(signers[i]).String(), i)
		}

		// Charge gas for the signature, as ante.SigGasConsumeDecorator would
		// have done had the ante chain not been short-circuited.
		if err := aid.sigGasConsumer(ctx.GasMeter(), signing.SignatureV2{
			PubKey:   pubKey,
			Data:     sig.Data,
			Sequence: sig.Sequence,
		}, params); err != nil {
			return err
		}

		// retrieve signer data
		chainID := ctx.ChainID()
		var accSequence uint64 = 0
		var accNum uint64 = 0

		// no need to verify signatures on recheck tx
		if !simulate && !ctx.IsReCheckTx() && ctx.IsSigverifyTx() {
			anyPk, _ := codectypes.NewAnyWithValue(pubKey)

			signerData := txsigning.SignerData{
				Address:       newAccAddr.String(),
				ChainID:       chainID,
				AccountNumber: accNum,
				Sequence:      accSequence,
				PubKey: &anypb.Any{
					TypeUrl: anyPk.TypeUrl,
					Value:   anyPk.Value,
				},
			}
			adaptableTx, ok := tx.(authsigning.V2AdaptableTx)
			if !ok {
				return fmt.Errorf("expected tx to implement V2AdaptableTx, got %T", tx)
			}
			txData := adaptableTx.GetSigningTxData()
			ctx.Logger().Debug("account init decorator: verifying signature for new account",
				"address", newAccAddr.String(),
				"chain_id", chainID,
				"acc_num", accNum,
				"sequence", accSequence,
			)
			err = authsigning.VerifySignature(ctx, pubKey, signerData, sig.Data, aid.signModeHandler, txData)
			if err != nil {
				var errMsg string
				if OnlyLegacyAminoSigners(sig.Data) {
					// If all signers are using SIGN_MODE_LEGACY_AMINO, we rely on VerifySignature to check account sequence number,
					// and therefore communicate sequence number as a potential cause of error.
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d), sequence (%d) and chain-id (%s)", accNum, accSequence, chainID)
				} else {
					errMsg = fmt.Sprintf("signature verification failed; please verify account number (%d) and chain-id (%s): (%s)", accNum, chainID, err.Error())
				}
				ctx.Logger().Debug("account init decorator: signature invalid for new account",
					"address", newAccAddr.String(),
					"chain_id", chainID,
				)
				return errorsmod.Wrap(sdkerrors.ErrUnauthorized, errMsg)

			}
		} else {
			ctx.Logger().Debug("account init decorator: skipping signature verification",
				"address", newAccAddr.String(),
				"simulate", simulate,
				"is_recheck_tx", ctx.IsReCheckTx(),
				"is_sigverify_tx", ctx.IsSigverifyTx(),
			)
		}
	}
	return nil
}

// OnlyLegacyAminoSigners checks SignatureData to see if all
// signers are using SIGN_MODE_LEGACY_AMINO_JSON. If this is the case
// then the corresponding SignatureV2 struct will not have account sequence
// explicitly set, and we should skip the explicit verification of sig.Sequence
// in the SigVerificationDecorator's AnteHandler function.
func OnlyLegacyAminoSigners(sigData signing.SignatureData) bool {
	switch v := sigData.(type) {
	case *signing.SingleSignatureData:
		return v.SignMode == signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON
	case *signing.MultiSignatureData:
		for _, s := range v.Signatures {
			if !OnlyLegacyAminoSigners(s) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
