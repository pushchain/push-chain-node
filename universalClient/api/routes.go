package api

import "net/http"

// setupRoutes configures all HTTP routes for the API server
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	// API v1 endpoints
	mux.HandleFunc("/api/v1/chain-configs", s.handleChainData)

	return mux
}
