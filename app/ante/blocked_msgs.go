package ante

import (
	"fmt"

	errorsmod "cosmossdk.io/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
	errortypes "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
)

// maxNestedBlockedMsgs caps how deep the decorator recurses into nested
// authz.MsgExec messages while looking for blocked msg types.
const maxNestedBlockedMsgs = 7

// BlockedMsgsDecorator rejects a fixed set of msg type URLs anywhere in a tx:
// at the top level, and nested inside authz.MsgExec (arbitrarily deep, up to
// maxNestedBlockedMsgs).
//
// It complements cosmosante.NewAuthzLimiterDecorator, which only blocks msgs
// carried *inside* an authz message and lets the same msg through when it is
// submitted directly.
type BlockedMsgsDecorator struct {
	// blockedMsgTypes is the set of msg type URLs to reject.
	blockedMsgTypes map[string]struct{}
}

// NewBlockedMsgsDecorator creates a decorator that rejects the given msg type
// URLs regardless of where they appear in the tx.
func NewBlockedMsgsDecorator(blockedMsgTypes ...string) BlockedMsgsDecorator {
	blocked := make(map[string]struct{}, len(blockedMsgTypes))
	for _, msgType := range blockedMsgTypes {
		blocked[msgType] = struct{}{}
	}

	return BlockedMsgsDecorator{blockedMsgTypes: blocked}
}

func (bmd BlockedMsgsDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	if err := bmd.checkBlockedMsgs(tx.GetMsgs(), 1); err != nil {
		return ctx, errorsmod.Wrapf(errortypes.ErrUnauthorized, "%s", err.Error())
	}

	return next(ctx, tx, simulate)
}

// checkBlockedMsgs walks the msgs and returns an error on the first blocked msg
// type it finds. authz.MsgExec is unwrapped so a blocked msg cannot be smuggled
// through the authz module; authz.MsgGrant is checked so a grant for a blocked
// msg type cannot be created either.
func (bmd BlockedMsgsDecorator) checkBlockedMsgs(msgs []sdk.Msg, nestedLvl int) error {
	if nestedLvl >= maxNestedBlockedMsgs {
		return fmt.Errorf("found more nested msgs than permitted; got: %d, expected: <%d", nestedLvl, maxNestedBlockedMsgs)
	}

	for _, msg := range msgs {
		switch msg := msg.(type) {
		case *authz.MsgExec:
			innerMsgs, err := msg.GetMessages()
			if err != nil {
				return err
			}
			if err := bmd.checkBlockedMsgs(innerMsgs, nestedLvl+1); err != nil {
				return err
			}
		case *authz.MsgGrant:
			authorization, err := msg.GetAuthorization()
			if err != nil {
				return err
			}
			if err := bmd.rejectIfBlocked(authorization.MsgTypeURL()); err != nil {
				return err
			}
		default:
			if err := bmd.rejectIfBlocked(sdk.MsgTypeURL(msg)); err != nil {
				return err
			}
		}
	}

	return nil
}

func (bmd BlockedMsgsDecorator) rejectIfBlocked(msgTypeURL string) error {
	if _, blocked := bmd.blockedMsgTypes[msgTypeURL]; blocked {
		return fmt.Errorf("found blocked msg type: %s", msgTypeURL)
	}

	return nil
}
