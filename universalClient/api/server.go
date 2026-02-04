package api

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Server provides HTTP endpoints
type Server struct {
	logger zerolog.Logger
	server *http.Server
}

// NewServer creates a new Server instance
func NewServer(logger zerolog.Logger, port int) *Server {
	s := &Server{
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

	// Channel to signal server startup result
	startupChan := make(chan error, 1)

	// Start server in goroutine
	go func() {
		// Create a test listener to verify the port is available
		ln, err := net.Listen("tcp", s.server.Addr)
		if err != nil {
			startupChan <- fmt.Errorf("failed to bind to address %s: %w", s.server.Addr, err)
			return
		}
		ln.Close()

		// Signal successful startup check
		startupChan <- nil

		// Now start the actual server
		err = s.server.ListenAndServe()
		switch err {
		case nil:
			s.logger.Info().Msg("Query server stopped normally")
		case http.ErrServerClosed:
			s.logger.Info().Msg("Query server closed gracefully")
		default:
			s.logger.Error().Err(err).Msg("Query server error")
		}
	}()

	// Wait for startup result with timeout
	select {
	case err := <-startupChan:
		if err != nil {
			return err
		}
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("server startup timeout")
	}
}

// Stop gracefully shuts down the HTTP server
func (s *Server) Stop() error {
	if s.server != nil {
		return s.server.Close()
	}
	return nil
}
