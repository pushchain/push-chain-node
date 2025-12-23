package utils

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/app"
	"github.com/stretchr/testify/require"

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
) error {
	t.Helper()

	// Encode the real outbound tx_id (this is what validators vote on)
	txIDHex, err := uexecutortypes.EncodeOutboundTxIDHex(utxId, outbound.Id)
	require.NoError(t, err)

	observed := &uexecutortypes.OutboundObservation{
		Success:     success,
		ErrorMsg:    errorMsg,
		TxHash:      fmt.Sprintf("0xobserved-%s", outbound.Id),
		BlockHeight: 1,
	}

	msg := &uexecutortypes.MsgVoteOutbound{
		Signer:     coreValAddr,
		TxId:       txIDHex,
		ObservedTx: observed,
	}

	execMsg := authz.NewMsgExec(
		sdk.MustAccAddressFromBech32(universalAddr),
		[]sdk.Msg{msg},
	)

	_, err = app.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}

// ExecVoteGasPrice executes a MsgVoteGasPrice on behalf of the core validator
// through the universal validator using authz Exec.
func ExecVoteGasPrice(
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

	// Construct MsgVoteGasPrice with core validator as signer
	msg := &uexecutortypes.MsgVoteGasPrice{
		Signer:          coreValAddr,
		ObservedChainId: chainID,
		Price:           price,
		BlockNumber:     blockNumber,
	}

	// Wrap it inside MsgExec (executed by universal validator)
	execMsg := authz.NewMsgExec(
		sdk.MustAccAddressFromBech32(universalAddr),
		[]sdk.Msg{msg},
	)

	// Execute the authorization
	_, err := app.AuthzKeeper.Exec(ctx, &execMsg)
	return err
}
