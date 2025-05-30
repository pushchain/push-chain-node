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
	txHash string,
	caipAddress string,
) *MsgVerifyExternalTransaction {
	return &MsgVerifyExternalTransaction{
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
	// Since we removed the sender field, this message doesn't require a signer
	// For governance messages or permissionless interactions, we can return an empty slice or a module address
	return []sdk.AccAddress{} // Empty slice - no signer required
}

// GetSignBytes implements the sdk.Msg interface
func (m MsgVerifyExternalTransaction) GetSignBytes() []byte {
	bz, err := proto.Marshal(&m)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}
