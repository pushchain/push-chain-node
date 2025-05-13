package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	proto "github.com/cosmos/gogoproto/proto"
)

const (
	TypeMsgVerifyExternalTransaction = "verify_external_transaction"
)

// NewMsgVerifyExternalTransaction creates a new MsgVerifyExternalTransaction instance
func NewMsgVerifyExternalTransaction(
	sender sdk.AccAddress,
	txHash string,
	caipAddress string,
) *MsgVerifyExternalTransaction {
	return &MsgVerifyExternalTransaction{
		Sender:      sender.String(),
		TxHash:      txHash,
		CaipAddress: caipAddress,
	}
}

// Route implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) Route() string { return RouterKey }

// Type implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) Type() string { return TypeMsgVerifyExternalTransaction }

// ValidateBasic implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) ValidateBasic() error {
	if m.Sender == "" {
		return fmt.Errorf("sender address cannot be empty")
	}

	if _, err := sdk.AccAddressFromBech32(m.Sender); err != nil {
		return fmt.Errorf("invalid sender address: %w", err)
	}

	if m.TxHash == "" {
		return fmt.Errorf("transaction hash cannot be empty")
	}

	if m.CaipAddress == "" {
		return fmt.Errorf("CAIP address cannot be empty")
	}

	// Parse CAIP address for basic validation
	_, err := ParseCAIPAddress(m.CaipAddress)
	if err != nil {
		return fmt.Errorf("invalid CAIP address: %w", err)
	}

	return nil
}

// GetSigners implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) GetSigners() []sdk.AccAddress {
	sender, err := sdk.AccAddressFromBech32(m.Sender)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{sender}
}

// GetSignBytes implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) GetSignBytes() []byte {
	bz, err := proto.Marshal(&m)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}
