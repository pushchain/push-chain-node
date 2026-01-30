package types

import (
	"encoding/json"
	fmt "fmt"

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
	// Validate Id is non-empty
	if len(p.Id) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "id cannot be empty")
	}

	// Validate inbound_tx
	if err := p.InboundTx.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid inbound_tx")
	}

	// Validate pc_tx
	// Validate each pc_tx
	for i, tx := range p.PcTx {
		if tx == nil {
			return fmt.Errorf("pc_tx[%d] is nil", i)
		}
		if err := tx.ValidateBasic(); err != nil {
			return errors.Wrapf(err, "invalid pc_tx at index %d", i)
		}
	}

	// Validate outbound_tx
	// Validate each outbound_tx
	for i, tx := range p.OutboundTx {
		if tx == nil {
			return fmt.Errorf("pc_tx[%d] is nil", i)
		}
		if err := tx.ValidateBasic(); err != nil {
			return errors.Wrapf(err, "invalid outbound_tx at index %d", i)
		}
	}

	// Validate universal_status (must be a valid enum)
	if _, ok := UniversalTxStatus_name[int32(p.UniversalStatus)]; !ok {
		return errors.Wrapf(sdkerrors.ErrInvalidRequest, "invalid universal_status: %v", p.UniversalStatus)
	}

	return nil
}
