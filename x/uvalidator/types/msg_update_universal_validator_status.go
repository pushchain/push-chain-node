package types

import (
	"fmt"

	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var (
	_ sdk.Msg = &MsgUpdateUniversalValidatorStatus{}
)

// NewMsgUpdateUniversalValidatorStatus creates new instance of MsgUpdateUniversalValidatorStatus
func NewMsgUpdateUniversalValidatorStatus(
	sender sdk.Address,
	coreValidatorAddress sdk.Address,
	newUvStatus UVStatus,
) *MsgUpdateUniversalValidatorStatus {
	return &MsgUpdateUniversalValidatorStatus{
		Signer:               sender.String(),
		CoreValidatorAddress: coreValidatorAddress.String(),
		NewStatus:            newUvStatus,
	}
}

// Route returns the name of the module
func (msg MsgUpdateUniversalValidatorStatus) Route() string { return ModuleName }

// Type returns the action
func (msg MsgUpdateUniversalValidatorStatus) Type() string {
	return "update_universal_validator_status"
}

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgUpdateUniversalValidatorStatus) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgUpdateUniversalValidatorStatus message.
func (msg *MsgUpdateUniversalValidatorStatus) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

func (msg *MsgUpdateUniversalValidatorStatus) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate core validator address (must be a valid valoper address)
	_, err := sdk.ValAddressFromBech32(msg.CoreValidatorAddress)
	if err != nil {
		return errors.Wrap(err, "invalid core validator address")
	}

	// Validate new status
	// For now, Admin can only mutate a UV's status from PENDING_LEAVE to ACTIVE
	if msg.NewStatus != UVStatus_UV_STATUS_ACTIVE {
		return fmt.Errorf("invalid new status")
	}

	return nil
}
