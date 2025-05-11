package types

import (
	"strings"

	sdkerrors "cosmossdk.io/errors"
	"github.com/cosmos/cosmos-sdk/codec/legacy"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/migrations/legacytx"
)

// RouterKey is the message route for the USVL module
const RouterKey = ModuleName

// Message types for the usvl module
const (
	TypeMsgAddChainConfig    = "add_chain_config"
	TypeMsgUpdateChainConfig = "update_chain_config"
	TypeMsgDeleteChainConfig = "delete_chain_config"
)

// Error codes for the USVL module
const (
	BaseErrorCode uint32 = 1
)

var (
	ErrInvalidAddress = sdkerrors.Register(ModuleName, BaseErrorCode+1, "invalid address")
	ErrInvalidRequest = sdkerrors.Register(ModuleName, BaseErrorCode+2, "invalid request")
)

var (
	_ sdk.Msg            = &MsgAddChainConfig{}
	_ sdk.Msg            = &MsgUpdateChainConfig{}
	_ sdk.Msg            = &MsgDeleteChainConfig{}
	_ legacytx.LegacyMsg = &MsgAddChainConfig{}
	_ legacytx.LegacyMsg = &MsgUpdateChainConfig{}
	_ legacytx.LegacyMsg = &MsgDeleteChainConfig{}
)

// NewMsgAddChainConfig creates a new MsgAddChainConfig instance
func NewMsgAddChainConfig(
	authority string,
	chainId string,
	chainName string,
	caipPrefix string,
	lockerContractAddress string,
	usdcAddress string,
	publicRpcUrl string,
) *MsgAddChainConfig {
	return &MsgAddChainConfig{
		Authority: authority,
		ChainConfig: ChainConfig{
			ChainId:               chainId,
			ChainName:             chainName,
			CaipPrefix:            caipPrefix,
			LockerContractAddress: lockerContractAddress,
			UsdcAddress:           usdcAddress,
			PublicRpcUrl:          publicRpcUrl,
		},
	}
}

// Route implements the sdk.Msg interface
func (m MsgAddChainConfig) Route() string { return RouterKey }

// Type implements the sdk.Msg interface
func (m MsgAddChainConfig) Type() string { return TypeMsgAddChainConfig }

// GetSigners implements the sdk.Msg interface
func (m MsgAddChainConfig) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{authority}
}

// GetSignBytes implements the legacytx.LegacyMsg interface
func (m MsgAddChainConfig) GetSignBytes() []byte {
	bz, err := legacy.Cdc.MarshalJSON(&m)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the sdk.Msg interface
func (m MsgAddChainConfig) ValidateBasic() error {
	// Validate authority address
	_, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return sdkerrors.Wrapf(ErrInvalidAddress, "invalid authority address: %s", err)
	}

	// Validate chain config by converting to internal type
	config := ChainConfigDataFromProto(m.ChainConfig)
	return config.Validate()
}

// NewMsgUpdateChainConfig creates a new MsgUpdateChainConfig instance
func NewMsgUpdateChainConfig(
	authority string,
	chainId string,
	chainName string,
	caipPrefix string,
	lockerContractAddress string,
	usdcAddress string,
	publicRpcUrl string,
) *MsgUpdateChainConfig {
	return &MsgUpdateChainConfig{
		Authority: authority,
		ChainConfig: ChainConfig{
			ChainId:               chainId,
			ChainName:             chainName,
			CaipPrefix:            caipPrefix,
			LockerContractAddress: lockerContractAddress,
			UsdcAddress:           usdcAddress,
			PublicRpcUrl:          publicRpcUrl,
		},
	}
}

// Route implements the sdk.Msg interface
func (m MsgUpdateChainConfig) Route() string { return RouterKey }

// Type implements the sdk.Msg interface
func (m MsgUpdateChainConfig) Type() string { return TypeMsgUpdateChainConfig }

// GetSigners implements the sdk.Msg interface
func (m MsgUpdateChainConfig) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{authority}
}

// GetSignBytes implements the legacytx.LegacyMsg interface
func (m MsgUpdateChainConfig) GetSignBytes() []byte {
	bz, err := legacy.Cdc.MarshalJSON(&m)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the sdk.Msg interface
func (m MsgUpdateChainConfig) ValidateBasic() error {
	// Validate authority address
	_, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return sdkerrors.Wrapf(ErrInvalidAddress, "invalid authority address: %s", err)
	}

	// Validate chain config by converting to internal type
	config := ChainConfigDataFromProto(m.ChainConfig)
	return config.Validate()
}

// NewMsgDeleteChainConfig creates a new MsgDeleteChainConfig instance
func NewMsgDeleteChainConfig(authority string, chainId string) *MsgDeleteChainConfig {
	return &MsgDeleteChainConfig{
		Authority: authority,
		ChainId:   chainId,
	}
}

// Route implements the sdk.Msg interface
func (m MsgDeleteChainConfig) Route() string { return RouterKey }

// Type implements the sdk.Msg interface
func (m MsgDeleteChainConfig) Type() string { return TypeMsgDeleteChainConfig }

// GetSigners implements the sdk.Msg interface
func (m MsgDeleteChainConfig) GetSigners() []sdk.AccAddress {
	authority, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		panic(err)
	}
	return []sdk.AccAddress{authority}
}

// GetSignBytes implements the legacytx.LegacyMsg interface
func (m MsgDeleteChainConfig) GetSignBytes() []byte {
	bz, err := legacy.Cdc.MarshalJSON(&m)
	if err != nil {
		panic(err)
	}
	return sdk.MustSortJSON(bz)
}

// ValidateBasic implements the sdk.Msg interface
func (m MsgDeleteChainConfig) ValidateBasic() error {
	// Validate authority address
	_, err := sdk.AccAddressFromBech32(m.Authority)
	if err != nil {
		return sdkerrors.Wrapf(ErrInvalidAddress, "invalid authority address: %s", err)
	}

	// Validate chain ID is not empty
	if strings.TrimSpace(m.ChainId) == "" {
		return sdkerrors.Wrap(ErrInvalidRequest, "chain ID cannot be empty")
	}

	return nil
}
