package api

import "net/http"

// setupRoutes configures all HTTP routes for the API server
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	// API v1 endpoints
	mux.HandleFunc("/api/v1/chain-configs", s.handleChainConfigs)
	mux.HandleFunc("/api/v1/token-configs", s.handleTokenConfigs)
	mux.HandleFunc("/api/v1/token-configs-by-chain", s.handleTokenConfigsByChain)
	mux.HandleFunc("/api/v1/token-config", s.handleTokenConfig)

	return mux
}