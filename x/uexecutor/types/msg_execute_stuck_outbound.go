package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgExecuteStuckOutbound{}
)

// ValidateBasic mirrors MsgVoteOutbound: the admin must supply exactly the
// observation the validators voted on.
func (msg *MsgExecuteStuckOutbound) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	if strings.TrimSpace(msg.TxId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_id cannot be empty")
	}

	if strings.TrimSpace(msg.UtxId) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "utx_id cannot be empty")
	}

	if msg.ObservedTx == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "observed_tx cannot be nil")
	}

	return msg.ObservedTx.ValidateBasic()
}
