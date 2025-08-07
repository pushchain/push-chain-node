package api

import "time"

// QueryResponse represents the standard query response format
type QueryResponse struct {
	Data        interface{} `json:"data"`
	LastFetched time.Time   `json:"last_fetched"`
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error string `json:"error"`
}