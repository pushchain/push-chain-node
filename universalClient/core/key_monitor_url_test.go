package core

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestURLParsing(t *testing.T) {
	tests := []struct {
		name        string
		grpcURL     string
		expectedRPC string
	}{
		{
			name:        "HTTPS URL with port 443",
			grpcURL:     "https://grpc.rpc-testnet-donut-node1.push.org:443",
			expectedRPC: "http://grpc.rpc-testnet-donut-node1.push.org:26657",
		},
		{
			name:        "HTTPS URL without port",
			grpcURL:     "https://grpc.rpc-testnet-donut-node1.push.org",
			expectedRPC: "http://grpc.rpc-testnet-donut-node1.push.org:26657",
		},
		{
			name:        "HTTP URL with custom port",
			grpcURL:     "http://localhost:9090",
			expectedRPC: "http://localhost:26657",
		},
		{
			name:        "Plain hostname",
			grpcURL:     "grpc.example.com",
			expectedRPC: "http://grpc.example.com:26657",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the URL parsing logic from key_monitor.go
			parsedURL, err := url.Parse(tt.grpcURL)
			var hostname string

			if err != nil {
				// Fallback parsing for plain hostnames
				hostname = strings.TrimPrefix(tt.grpcURL, "https://")
				hostname = strings.TrimPrefix(hostname, "http://")
				if colonIndex := strings.Index(hostname, ":"); colonIndex > 0 {
					hostname = hostname[:colonIndex]
				}
			} else {
				hostname = parsedURL.Hostname()
				if hostname == "" {
					// Additional fallback
					hostname = strings.TrimPrefix(tt.grpcURL, "https://")
					hostname = strings.TrimPrefix(hostname, "http://")
					if colonIndex := strings.Index(hostname, ":"); colonIndex > 0 {
						hostname = hostname[:colonIndex]
					}
				}
			}

			rpcURL := fmt.Sprintf("http://%s:26657", hostname)
			assert.Equal(t, tt.expectedRPC, rpcURL, "RPC URL should match expected format")
		})
	}
}