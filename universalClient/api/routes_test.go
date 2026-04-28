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

	testCases := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
	}{
		{
			name:           "GET /health is allowed",
			method:         http.MethodGet,
			path:           "/health",
			expectedStatus: http.StatusOK,
		},
		{
			name:           "POST /health is rejected",
			method:         http.MethodPost,
			path:           "/health",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PUT /health is rejected",
			method:         http.MethodPut,
			path:           "/health",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "DELETE /health is rejected",
			method:         http.MethodDelete,
			path:           "/health",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "PATCH /health is rejected",
			method:         http.MethodPatch,
			path:           "/health",
			expectedStatus: http.StatusMethodNotAllowed,
		},
		{
			name:           "Non-existent endpoint returns 404",
			method:         http.MethodGet,
			path:           "/api/v1/non-existent",
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			assert.Equal(t, tc.expectedStatus, w.Code)
		})
	}
}
