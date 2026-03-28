package api

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Server provides HTTP endpoints
type Server struct {
	logger   zerolog.Logger
	server   *http.Server
	listener net.Listener
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

	ln, err := net.Listen("tcp", s.server.Addr)
	if err != nil {
		return fmt.Errorf("failed to bind to address %s: %w", s.server.Addr, err)
	}
	s.listener = ln

	go func() {
		err := s.server.Serve(ln)
		switch err {
		case nil:
			s.logger.Info().Msg("Query server stopped normally")
		case http.ErrServerClosed:
			s.logger.Info().Msg("Query server closed gracefully")
		default:
			s.logger.Error().Err(err).Msg("Query server error")
		}
	}()

	return nil
}

// Addr returns the listener address, useful when started on port 0
func (s *Server) Addr() string {
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// Stop gracefully shuts down the HTTP server
func (s *Server) Stop() error {
	if s.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.server.Shutdown(ctx)
	}
	return nil
}
