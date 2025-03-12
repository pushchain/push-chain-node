package app

import (
	// "github.com/cosmos/cosmos-sdk/codec"
	// "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	// "github.com/cosmos/cosmos-sdk/x/auth/tx"
	evmenc "github.com/zeta-chain/ethermint/encoding"
	ethermint "github.com/zeta-chain/ethermint/types"

	// "pushchain/app/params"
)

// makeEncodingConfig creates an EncodingConfig for an amino based test configuration.
// func makeEncodingConfig() params.EncodingConfig {
// 	amino := codec.NewLegacyAmino()
// 	interfaceRegistry := types.NewInterfaceRegistry()
// 	marshaler := codec.NewProtoCodec(interfaceRegistry)
// 	txCfg := tx.NewTxConfig(marshaler, tx.DefaultSignModes)

// 	return params.EncodingConfig{
// 		InterfaceRegistry: interfaceRegistry,
// 		Marshaler:         marshaler,
// 		TxConfig:          txCfg,
// 		Amino:             amino,
// 	}
// }

// MakeEncodingConfig creates an EncodingConfig for testing
func MakeEncodingConfig() ethermint.EncodingConfig {
	encodingConfig := evmenc.MakeConfig(ModuleBasics)
	// std.RegisterLegacyAminoCodec(encodingConfig.Amino)
	std.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	// ModuleBasics.RegisterLegacyAminoCodec(encodingConfig.Amino)
	ModuleBasics.RegisterInterfaces(encodingConfig.InterfaceRegistry)
	return encodingConfig
}
