package api

import "net/http"

// setupRoutes configures all HTTP routes for the API server
func (s *Server) setupRoutes() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoint — GET only; other methods return 405 Method Not Allowed.
	mux.HandleFunc("GET /health", s.handleHealth)

	return mux
}
