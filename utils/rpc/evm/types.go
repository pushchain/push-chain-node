package rpc

// Transaction represents a basic EVM transaction response from eth_getTransactionByHash
type Transaction struct {
	BlockHash        string `json:"blockHash"`
	BlockNumber      string `json:"blockNumber"`
	From             string `json:"from"`
	Gas              string `json:"gas"`
	GasPrice         string `json:"gasPrice"`
	Hash             string `json:"hash"`
	Input            string `json:"input"`
	Nonce            string `json:"nonce"`
	To               string `json:"to"`
	TransactionIndex string `json:"transactionIndex"`
	Value            string `json:"value"`
	V                string `json:"v"`
	R                string `json:"r"`
	S                string `json:"s"`
}

// TransactionReceipt represents eth_getTransactionReceipt result
type TransactionReceipt struct {
	TransactionHash   string        `json:"transactionHash"`
	TransactionIndex  string        `json:"transactionIndex"`
	BlockHash         string        `json:"blockHash"`
	BlockNumber       string        `json:"blockNumber"`
	From              string        `json:"from"`
	To                string        `json:"to"`
	CumulativeGasUsed string        `json:"cumulativeGasUsed"`
	GasUsed           string        `json:"gasUsed"`
	ContractAddress   string        `json:"contractAddress"`
	Logs              []interface{} `json:"logs"` // can define `Log` struct if needed
	Status            string        `json:"status"`
}

// Block represents eth_getBlockByNumber response

type Block struct {
	Number       string        `json:"number"`
	Hash         string        `json:"hash"`
	Timestamp    string        `json:"timestamp"`
	Transactions []interface{} `json:"transactions"` // list of tx hashes or full txs depending on fullTx flag
}
