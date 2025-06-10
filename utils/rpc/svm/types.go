package rpc

// Transaction represents a Solana transaction response from getTransaction
type Transaction struct {
	Slot        uint64 `json:"slot"`
	Transaction struct {
		Message struct {
			AccountKeys  []string `json:"accountKeys"`
			Instructions []struct {
				ProgramIDIndex int    `json:"programIdIndex"`
				Accounts       []int  `json:"accounts"`
				Data           string `json:"data"`
			} `json:"instructions"`
		} `json:"message"`
	} `json:"transaction"`
	Meta struct {
		Err         interface{} `json:"err"`
		LogMessages []string    `json:"logMessages"`
	} `json:"meta"`
}

// Slot represents a Solana slot response from getSlot
type Slot uint64
