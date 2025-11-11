package core

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

// HealthStatus represents the health status of the TSS service.
type HealthStatus struct {
	Healthy     bool
	ActiveSessions int
	LastError   error
	LastErrorTime time.Time
	mu          sync.RWMutex
}

// HealthMonitor provides health checking capabilities for the TSS service.
type HealthMonitor struct {
	service *Service
	status  *HealthStatus
	logger  zerolog.Logger
}

// NewHealthMonitor creates a new health monitor.
func NewHealthMonitor(service *Service, logger zerolog.Logger) *HealthMonitor {
	return &HealthMonitor{
		service: service,
		status:  &HealthStatus{Healthy: true},
		logger:  logger,
	}
}

// CheckHealth performs a health check on the TSS service.
func (h *HealthMonitor) CheckHealth(ctx context.Context) *HealthStatus {
	h.status.mu.RLock()
	defer h.status.mu.RUnlock()

	// Count active sessions
	activeSessions := h.service.getActiveSessionCount()

	// Create a copy of the status
	status := &HealthStatus{
		Healthy:        h.status.Healthy,
		ActiveSessions: activeSessions,
		LastError:      h.status.LastError,
		LastErrorTime:  h.status.LastErrorTime,
	}

	// Consider unhealthy if there are too many active sessions (potential resource leak)
	if activeSessions > 100 {
		status.Healthy = false
	}

	return status
}

// RecordError records an error for health monitoring.
func (h *HealthMonitor) RecordError(err error) {
	h.status.mu.Lock()
	defer h.status.mu.Unlock()

	h.status.LastError = err
	h.status.LastErrorTime = time.Now()

	// Mark unhealthy if we have recent errors
	if err != nil {
		h.status.Healthy = false
	} else {
		// Reset to healthy if no error
		h.status.Healthy = true
	}
}

// GetStatus returns the current health status.
func (h *HealthMonitor) GetStatus() *HealthStatus {
	h.status.mu.RLock()
	defer h.status.mu.RUnlock()

	return &HealthStatus{
		Healthy:        h.status.Healthy,
		ActiveSessions: h.status.ActiveSessions,
		LastError:      h.status.LastError,
		LastErrorTime:  h.status.LastErrorTime,
	}
}

// Add a method to Service to get active session count
func (s *Service) getActiveSessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

