package types

import "math/big"

// EVMFundsAddedEventData represents decoded data from the `FundsAdded` EVM event.
type EVMFundsAddedEventData struct {
	AmountInUSD *big.Int
	Decimals    uint32
	PayloadHash string
}

// SVMFundsAddedEventData represents decoded FundsAdded event from Solana
type SVMFundsAddedEventData struct {
	AmountInUSD *big.Int
	Decimals    uint32
	PayloadHash string // hex-encoded
}
