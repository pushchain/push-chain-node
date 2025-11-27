package types

import (
	"cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	_ sdk.Msg = &MsgMigrateUEA{}
)

// NewMsgMigrateUEA creates new instance of MsgMigrateUEA
func NewMsgMigrateUEA(
	sender sdk.Address,
	universalAccountId *UniversalAccountId,
	migrationPayload *MigrationPayload,
	signature string,
) *MsgMigrateUEA {
	return &MsgMigrateUEA{
		Signer:             sender.String(),
		UniversalAccountId: universalAccountId,
		MigrationPayload:   migrationPayload,
		Signature:          signature,
	}
}

// Route returns the name of the module
func (msg MsgMigrateUEA) Route() string { return ModuleName }

// Type returns the action
func (msg MsgMigrateUEA) Type() string { return "migrate_uea" }

// GetSignBytes implements the LegacyMsg interface.
func (msg MsgMigrateUEA) GetSignBytes() []byte {
	return sdk.MustSortJSON(AminoCdc.MustMarshalJSON(&msg))
}

// GetSigners returns the expected signers for a MsgExecutePayload message.
func (msg *MsgMigrateUEA) GetSigners() []sdk.AccAddress {
	addr, _ := sdk.AccAddressFromBech32(msg.Signer)
	return []sdk.AccAddress{addr}
}

// ValidateBasic does a sanity check on the provided data.
func (msg *MsgMigrateUEA) ValidateBasic() error {
	// Validate signer
	if _, err := sdk.AccAddressFromBech32(msg.Signer); err != nil {
		return errors.Wrap(err, "invalid signer address")
	}

	// Validate universalAccountId
	if msg.UniversalAccountId == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "universal account cannot be nil")
	}

	// Validate migration payload
	if msg.MigrationPayload == nil {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "migration payload cannot be nil")
	}

	// Validate Signature
	if len(msg.Signature) == 0 {
		return errors.Wrap(sdkerrors.ErrInvalidRequest, "signature cannot be empty")
	}

	// Validate universalAccountId structure
	if err := msg.UniversalAccountId.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid universalAccountId")
	}

	// Validate migration payload structure
	if err := msg.MigrationPayload.ValidateBasic(); err != nil {
		return errors.Wrap(err, "invalid migration payload")
	}

	return nil
}
