package testutils

import (
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
