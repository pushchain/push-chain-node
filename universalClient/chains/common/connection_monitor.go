package common

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// ConnectionState represents the connection state
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateReconnecting
)

// ConnectionMonitor monitors connection health and handles reconnections
type ConnectionMonitor struct {
	mu              sync.RWMutex
	state           ConnectionState
	lastHealthCheck time.Time
	healthInterval  time.Duration
	logger          zerolog.Logger
	onReconnect     func() error
	retryManager    *RetryManager
	stopCh          chan struct{}
	wg              sync.WaitGroup
}

// NewConnectionMonitor creates a new connection monitor
func NewConnectionMonitor(
	healthInterval time.Duration,
	onReconnect func() error,
	logger zerolog.Logger,
) *ConnectionMonitor {
	if healthInterval <= 0 {
		healthInterval = 30 * time.Second
	}

	return &ConnectionMonitor{
		state:          StateDisconnected,
		healthInterval: healthInterval,
		logger:         logger.With().Str("component", "connection_monitor").Logger(),
		onReconnect:    onReconnect,
		retryManager:   NewRetryManager(DefaultRetryConfig(), logger),
		stopCh:         make(chan struct{}),
	}
}

// Start starts the connection monitor
func (m *ConnectionMonitor) Start(ctx context.Context, healthCheck func() error) {
	m.mu.Lock()
	if m.state == StateConnected {
		m.mu.Unlock()
		return
	}
	m.state = StateConnecting
	m.mu.Unlock()

	m.wg.Add(1)
	go m.monitorLoop(ctx, healthCheck)
}

// Stop stops the connection monitor
func (m *ConnectionMonitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
	
	m.mu.Lock()
	m.state = StateDisconnected
	m.mu.Unlock()
}

// GetState returns the current connection state
func (m *ConnectionMonitor) GetState() ConnectionState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsConnected returns true if connected
func (m *ConnectionMonitor) IsConnected() bool {
	return m.GetState() == StateConnected
}

// SetConnected marks the connection as connected
func (m *ConnectionMonitor) SetConnected() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.state != StateConnected {
		m.logger.Info().Msg("connection established")
		m.state = StateConnected
		m.lastHealthCheck = time.Now()
	}
}

// SetDisconnected marks the connection as disconnected
func (m *ConnectionMonitor) SetDisconnected() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.state == StateConnected {
		m.logger.Warn().Msg("connection lost")
		m.state = StateDisconnected
	}
}

// monitorLoop continuously monitors connection health
func (m *ConnectionMonitor) monitorLoop(ctx context.Context, healthCheck func() error) {
	defer m.wg.Done()

	ticker := time.NewTicker(m.healthInterval)
	defer ticker.Stop()

	// Initial connection attempt
	m.handleReconnection(ctx)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info().Msg("stopping connection monitor: context cancelled")
			return
		case <-m.stopCh:
			m.logger.Info().Msg("stopping connection monitor")
			return
		case <-ticker.C:
			// Perform health check
			if err := healthCheck(); err != nil {
				m.logger.Error().
					Err(err).
					Msg("health check failed")
				
				m.SetDisconnected()
				m.handleReconnection(ctx)
			} else {
				m.SetConnected()
				m.mu.Lock()
				m.lastHealthCheck = time.Now()
				m.mu.Unlock()
			}
		}
	}
}

// handleReconnection handles reconnection with retry logic
func (m *ConnectionMonitor) handleReconnection(ctx context.Context) {
	m.mu.Lock()
	if m.state == StateConnected || m.state == StateReconnecting {
		m.mu.Unlock()
		return
	}
	m.state = StateReconnecting
	m.mu.Unlock()

	m.logger.Info().Msg("attempting reconnection")

	// Use retry manager for reconnection attempts
	err := m.retryManager.ExecuteWithRetry(ctx, "reconnection", func() error {
		if m.onReconnect != nil {
			if err := m.onReconnect(); err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		m.logger.Error().
			Err(err).
			Msg("failed to reconnect after all attempts")
		m.SetDisconnected()
	} else {
		m.SetConnected()
		m.logger.Info().Msg("successfully reconnected")
	}
}

// WaitForConnection waits until connected or context expires
func (m *ConnectionMonitor) WaitForConnection(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if m.IsConnected() {
				return nil
			}
		}
	}
}