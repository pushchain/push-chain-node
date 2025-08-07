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
	mockClient := NewMockUniversalClient()
	server := &Server{
		client: mockClient,
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
			name:           "Chain configs endpoint",
			path:           "/api/v1/chain-configs",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Token configs endpoint",
			path:           "/api/v1/token-configs",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Token configs by chain endpoint",
			path:           "/api/v1/token-configs-by-chain?chain=eip155:1",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "Token config endpoint",
			path:           "/api/v1/token-config?chain=eip155:1&address=0xAAA",
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