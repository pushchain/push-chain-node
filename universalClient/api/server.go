package api

import (
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Server provides HTTP endpoints for querying configuration data
type Server struct {
	client UniversalClientInterface
	logger zerolog.Logger
	server *http.Server
}

// NewServer creates a new Server instance
func NewServer(client UniversalClientInterface, logger zerolog.Logger, port int) *Server {
	s := &Server{
		client: client,
		logger: logger,
	}

	mux := s.setupRoutes()

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	return s
}

// Start starts the HTTP server
func (s *Server) Start() error {
	if s.server == nil {
		return fmt.Errorf("query server is nil")
	}

	// Start server in goroutine
	go func() {
		err := s.server.ListenAndServe()
		switch err {
		case nil:
			s.logger.Info().Msg("Query server stopped normally")
		case http.ErrServerClosed:
			s.logger.Info().Msg("Query server closed gracefully")
		default:
			s.logger.Error().Err(err).Msg("Query server error")
		}
	}()

	// Give the server time to start
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Stop gracefully shuts down the HTTP server
func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}