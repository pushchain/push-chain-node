//go:build test
// +build test

package app

import (
	evmtypes "github.com/cosmos/evm/x/vm/types"
)

// seedDefaultEvmCoinInfo is a no-op under the test build tag.
//
// There, SetDefaultEvmCoinInfo writes the single testing coin-info variable rather
// than a separate fallback, so seeding it here would make x/vm's InitGenesis panic
// with "EVM coin info already set". Test apps configure coin info themselves.
func seedDefaultEvmCoinInfo(_ evmtypes.EvmCoinInfo) {}
