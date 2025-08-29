package types

import (
	"encoding/json"

	"cosmossdk.io/errors"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

// Stringer method for Params.
func (p UniversalTx) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// ValidateBasic does the sanity check on the UniversalTx fields.
func (p UniversalTx) ValidateBasic() error {
	// Validate inbound_tx
	if err := p.InboundTx.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid inbound_tx")
	}

	// Validate pc_tx
	if err := p.PcTx.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid pc_tx")
	}

	// Validate outbound_tx
	if err := p.OutboundTx.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid outbound_tx")
	}

	// Validate universal_status (must be a valid enum)
	if _, ok := UniversalTxStatus_name[int32(p.UniversalStatus)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid universal_status: %v", p.UniversalStatus)
	}

	return nil
}
