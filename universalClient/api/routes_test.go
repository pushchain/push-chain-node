package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestSetupRoutes(t *testing.T) {
	logger := zerolog.New(zerolog.NewTestWriter(t))
	server := &Server{
		logger: logger,
	}

	mux := server.setupRoutes()

	// Test that all routes are registered correctly
	testCases := []struct {
		name           string
		path           string
		expectedStatus int
	}{
		{
			name:           "Health endpoint",
			path:           "/health",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Non-existent endpoint",
			path:           "/api/v1/non-existent",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
		})
	}
}
