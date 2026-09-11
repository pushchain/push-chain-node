package types

import (
	"encoding/json"
	"fmt"
)

// DefaultMaxGaslessTxGas is the default cap on the gas limit a fee-exempt
// (gasless) transaction may declare.
//
// Fee-paying transactions are self-limiting: the required fee is
// ceil(minGasPrice * gasLimit), so declaring a large gas limit costs real
// tokens. Gasless transactions pay nothing, so nothing bounds the gas they
// declare, and the declared gas is what accumulates into the block's
// cumulative gas wanted. 100,000,000 is roughly 20x the largest gas actually
// consumed by a gasless transaction observed on the network.
const DefaultMaxGaslessTxGas uint64 = 100_000_000

// DefaultParams returns default module parameters.
func DefaultParams() Params {
	// TODO:
	return Params{
		SomeValue:       true,
		MaxGaslessTxGas: DefaultMaxGaslessTxGas,
	}
}

// Stringer method for Params.
func (p Params) String() string {
	bz, err := json.Marshal(p)
	if err != nil {
		panic(err)
	}

	return string(bz)
}

// Validate does the sanity check on the params.
func (p Params) ValidateBasic() error {
	// A zero cap would reject every gasless transaction, which would stop the
	// universal validators from voting. Governance must always set a usable
	// value.
	if p.MaxGaslessTxGas == 0 {
		return fmt.Errorf("max_gasless_tx_gas must be greater than 0")
	}

	return nil
}
