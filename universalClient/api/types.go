package api

import "github.com/pushchain/push-chain-node/universalClient/cache"

type universalClientInterface interface {
	GetAllChainData() []*cache.ChainData
}

// QueryResponse represents the standard query response format
type queryResponse struct {
	Data interface{} `json:"data"`
}

// ErrorResponse represents an error response
type errorResponse struct {
	Error string `json:"error"`
}
