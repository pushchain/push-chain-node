package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateGRPCConnection(t *testing.T) {
	tests := []struct {
		name          string
		endpoint      string
		wantErr       bool
		errorContains string
	}{
		{
			name:          "empty endpoint",
			endpoint:      "",
			wantErr:       true,
			errorContains: "empty endpoint",
		},
		{
			name:     "http endpoint without port",
			endpoint: "http://localhost",
			wantErr:  false,
		},
		{
			name:     "https endpoint without port",
			endpoint: "https://localhost",
			wantErr:  false,
		},
		{
			name:     "http endpoint with port",
			endpoint: "http://localhost:9090",
			wantErr:  false,
		},
		{
			name:     "https endpoint with port",
			endpoint: "https://localhost:9090",
			wantErr:  false,
		},
		{
			name:     "endpoint without scheme and without port",
			endpoint: "localhost",
			wantErr:  false,
		},
		{
			name:     "endpoint without scheme but with port",
			endpoint: "localhost:9090",
			wantErr:  false,
		},
		{
			name:     "endpoint with custom port",
			endpoint: "localhost:8080",
			wantErr:  false,
		},
		{
			name:     "endpoint with invalid port format",
			endpoint: "localhost:",
			wantErr:  false, // Should add default port
		},
		{
			name:     "endpoint with path after colon",
			endpoint: "http://localhost:/path",
			wantErr:  false, // Should add default port
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
				assert.Nil(t, conn)
			} else {
				require.NoError(t, err)
				require.NotNil(t, conn)
				// Clean up connection
				err = conn.Close()
				assert.NoError(t, err)
			}
		})
	}
}

func TestCreateGRPCConnection_PortHandling(t *testing.T) {
	tests := []struct {
		name             string
		endpoint         string
		expectedContains string // What the processed endpoint should contain
	}{
		{
			name:             "adds default port when missing",
			endpoint:         "localhost",
			expectedContains: ":9090",
		},
		{
			name:             "preserves existing port",
			endpoint:         "localhost:8080",
			expectedContains: ":8080",
		},
		{
			name:             "adds port to http endpoint",
			endpoint:         "http://localhost",
			expectedContains: ":9090",
		},
		{
			name:             "adds port to https endpoint",
			endpoint:         "https://localhost",
			expectedContains: ":9090",
		},
		{
			name:             "handles empty port",
			endpoint:         "localhost:",
			expectedContains: ":9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)
			require.NoError(t, err)
			require.NotNil(t, conn)

			// Get the target from the connection
			target := conn.Target()
			assert.Contains(t, target, tt.expectedContains, "Expected target to contain %s, got %s", tt.expectedContains, target)

			// Clean up
			err = conn.Close()
			assert.NoError(t, err)
		})
	}
}

func TestCreateGRPCConnection_TLSHandling(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		// Note: We can't easily test if TLS is actually enabled without attempting a real connection
		// But we can verify the function doesn't error for different schemes
	}{
		{
			name:     "https should not error",
			endpoint: "https://localhost:9090",
		},
		{
			name:     "http should not error",
			endpoint: "http://localhost:9090",
		},
		{
			name:     "no scheme should not error",
			endpoint: "localhost:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, err := CreateGRPCConnection(tt.endpoint)
			require.NoError(t, err)
			require.NotNil(t, conn)

			// Clean up
			err = conn.Close()
			assert.NoError(t, err)
		})
	}
}

func TestExtractHostnameFromURL(t *testing.T) {
	tests := []struct {
		name             string
		url              string
		expectedHostname string
		wantErr          bool
		errorContains    string
	}{
		{
			name:             "https URL with port",
			url:              "https://grpc.example.com:443",
			expectedHostname: "grpc.example.com",
			wantErr:          false,
		},
		{
			name:             "https URL without port",
			url:              "https://grpc.example.com",
			expectedHostname: "grpc.example.com",
			wantErr:          false,
		},
		{
			name:             "http URL with port",
			url:              "http://localhost:9090",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "http URL without port",
			url:              "http://api.test.com",
			expectedHostname: "api.test.com",
			wantErr:          false,
		},
		{
			name:             "plain hostname without port",
			url:              "example.com",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "plain hostname with port",
			url:              "example.com:8080",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "localhost without port",
			url:              "localhost",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "localhost with port",
			url:              "localhost:9090",
			expectedHostname: "localhost",
			wantErr:          false,
		},
		{
			name:             "complex subdomain",
			url:              "https://grpc.rpc-testnet-donut-node1.push.org:443",
			expectedHostname: "grpc.rpc-testnet-donut-node1.push.org",
			wantErr:          false,
		},
		{
			name:             "URL with path",
			url:              "https://example.com:443/some/path",
			expectedHostname: "example.com",
			wantErr:          false,
		},
		{
			name:             "empty URL",
			url:              "",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "empty URL provided",
		},
		{
			name:             "URL with only scheme",
			url:              "https://",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "could not extract hostname",
		},
		{
			name:             "URL with only port",
			url:              ":9090",
			expectedHostname: "",
			wantErr:          true,
			errorContains:    "could not extract hostname",
		},
		{
			name:             "IPv4 address",
			url:              "192.168.1.1:9090",
			expectedHostname: "192.168.1.1",
			wantErr:          false,
		},
		{
			name:             "IPv4 address with scheme",
			url:              "http://192.168.1.1:9090",
			expectedHostname: "192.168.1.1",
			wantErr:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hostname, err := ExtractHostnameFromURL(tt.url)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedHostname, hostname)
			}
		})
	}
}