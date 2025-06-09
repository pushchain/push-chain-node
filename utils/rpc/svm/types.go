package rpc

// Transaction represents a Solana transaction response from getTransaction
type Transaction struct {
	BlockTime int64  `json:"blockTime"`
	Slot      uint64 `json:"slot"`
	Signature string `json:"signature"`
	Status    struct {
		Ok  interface{} `json:"Ok,omitempty"`
		Err interface{} `json:"Err,omitempty"`
	} `json:"status"`
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
		Err               interface{} `json:"err"`
		LogMessages       []string    `json:"logMessages"`
		PostTokenBalances []struct {
			Owner    string `json:"owner"`
			Writable bool   `json:"writable"`
		} `json:"postTokenBalances"`
	} `json:"meta"`
}

// Block represents a Solana block response from getBlock
type Block struct {
	Blockhash    string        `json:"blockhash"`
	BlockTime    int64         `json:"blockTime"`
	ParentSlot   uint64        `json:"parentSlot"`
	Slot         uint64        `json:"slot"`
	Transactions []Transaction `json:"transactions"`
}

// Slot represents a Solana slot response from getSlot
type Slot uint64
