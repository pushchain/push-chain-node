package utils

import (
	"fmt"
	"net/url"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

// CreateGRPCConnection creates a gRPC connection with appropriate transport security.
// It automatically detects whether to use TLS based on the URL scheme (https:// or http://).
// The function handles:
//   - https:// URLs: Uses TLS with default credentials
//   - http:// or no scheme: Uses insecure connection
//   - Automatically adds default port 9090 if no port is specified
//
// The endpoint is processed to remove the scheme prefix before dialing.
func CreateGRPCConnection(endpoint string) (*grpc.ClientConn, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("empty endpoint provided")
	}

	// Determine if we should use TLS and process the endpoint
	processedEndpoint := endpoint
	useTLS := false

	if strings.HasPrefix(endpoint, "https://") {
		processedEndpoint = strings.TrimPrefix(endpoint, "https://")
		useTLS = true
	} else if strings.HasPrefix(endpoint, "http://") {
		processedEndpoint = strings.TrimPrefix(endpoint, "http://")
		useTLS = false
	}

	// Add default port if not present
	if !strings.Contains(processedEndpoint, ":") {
		processedEndpoint = processedEndpoint + ":9090"
	} else {
		// Check if the port is valid (i.e., after the last colon is a number)
		lastColon := strings.LastIndex(processedEndpoint, ":")
		afterColon := processedEndpoint[lastColon+1:]
		if afterColon == "" || strings.Contains(afterColon, "/") {
			// No valid port, add default
			processedEndpoint = strings.TrimSuffix(processedEndpoint, ":") + ":9090"
		}
	}

	// Configure connection options
	var opts []grpc.DialOption
	if useTLS {
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(nil)))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	// Create the connection
	conn, err := grpc.NewClient(processedEndpoint, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create gRPC connection to %s: %w", processedEndpoint, err)
	}

	return conn, nil
}

// ExtractHostnameFromURL extracts the hostname from a URL string.
// It handles various URL formats including:
//   - Full URLs with scheme (https://example.com:443)
//   - URLs without scheme (example.com:9090)
//   - Plain hostnames (example.com)
//
// The function returns just the hostname without port or scheme.
func ExtractHostnameFromURL(grpcURL string) (string, error) {
	if grpcURL == "" {
		return "", fmt.Errorf("empty URL provided")
	}

	// Try to parse as a standard URL
	parsedURL, err := url.Parse(grpcURL)
	if err == nil && parsedURL.Hostname() != "" {
		// Successfully parsed and has a hostname
		return parsedURL.Hostname(), nil
	}

	// Fallback: Handle cases where url.Parse fails or returns empty hostname
	// This handles plain hostnames like "example.com" or "example.com:9090"
	hostname := grpcURL

	// Remove common schemes if present
	hostname = strings.TrimPrefix(hostname, "https://")
	hostname = strings.TrimPrefix(hostname, "http://")

	// Remove port if present (but check that there's something before the colon)
	if colonIndex := strings.Index(hostname, ":"); colonIndex >= 0 {
		if colonIndex == 0 {
			// URL starts with ":", no hostname
			return "", fmt.Errorf("could not extract hostname from URL: %s", grpcURL)
		}
		hostname = hostname[:colonIndex]
	}

	// Remove any trailing slashes
	hostname = strings.TrimSuffix(hostname, "/")

	if hostname == "" {
		return "", fmt.Errorf("could not extract hostname from URL: %s", grpcURL)
	}

	return hostname, nil
}