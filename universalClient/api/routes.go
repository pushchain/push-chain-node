package api

import "net/http"

// setupRoutes configures all HTTP routes for the API server
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	return mux
}
