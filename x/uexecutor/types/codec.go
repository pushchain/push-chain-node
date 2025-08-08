package types

import (
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

var (
	amino    = codec.NewLegacyAmino()
	AminoCdc = codec.NewAminoCodec(amino)
)

func init() {
	RegisterLegacyAminoCodec(amino)
	cryptocodec.RegisterCrypto(amino)
	sdk.RegisterLegacyAminoCodec(amino)
}

// RegisterLegacyAminoCodec registers concrete types on the LegacyAmino codec
func RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	cdc.RegisterConcrete(&MsgUpdateParams{}, ModuleName+"/MsgUpdateParams", nil)
	cdc.RegisterConcrete(&MsgDeployUEA{}, ModuleName+"/MsgDeployUEAResponse", nil)
	cdc.RegisterConcrete(&MsgMintPC{}, ModuleName+"/MsgMintPC", nil)
	cdc.RegisterConcrete(&MsgExecutePayload{}, ModuleName+"/MsgExecutePayload", nil)
	cdc.RegisterConcrete(&MsgAddChainConfig{}, ModuleName+"/MsgAddChainConfig", nil)
}

func RegisterInterfaces(registry types.InterfaceRegistry) {

	registry.RegisterImplementations(
		(*sdk.Msg)(nil),
		&MsgUpdateParams{},
		&MsgDeployUEAResponse{},
		&MsgMintPC{},
		&MsgExecutePayload{},
		&MsgAddChainConfig{},
	)

	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
