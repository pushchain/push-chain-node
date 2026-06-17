package types

import (
	"strings"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var _ sdk.Msg = &MsgVoteFundMigration{}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgVoteFundMigration) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}
	if msg.MigrationId == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "migration_id is required")
	}
	// A successful migration observation must carry the external tx hash.
	// Canonicalization (per the migration's chain namespace) happens in the
	// keeper, where the chain is known.
	if msg.Success && strings.TrimSpace(msg.TxHash) == "" {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "tx_hash is required when success is true")
	}
	return nil
}
