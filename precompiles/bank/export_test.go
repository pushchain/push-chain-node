package bank

import (
	"math/big"

	sdk "github.com/cosmos/cosmos-sdk/types"
	ethcommon "github.com/ethereum/go-ethereum/common"
)

// ABIJSON exposes the embedded ABI for tests.
func ABIJSON() []byte { return abiJSON }

// BurnForTest drives the burn logic without standing up an EVM. It mirrors what
// Run does after RunNativeAction has supplied the context and SetupABI has decoded
// the arguments, so the keeper interaction can be asserted directly.
func (p Precompile) BurnForTest(ctx sdk.Context, caller ethcommon.Address, amount *big.Int) ([]byte, error) {
	method := p.extABI.Methods[BurnMethod]
	return p.burnFrom(ctx, caller, &method, []interface{}{amount})
}
