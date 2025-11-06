package network

import (
	"context"

	"github.com/rs/zerolog"
)

// Network handles libp2p network connections and message routing
type Network struct {
	ctx    context.Context
	logger zerolog.Logger
}

// New creates a new network instance
func New(ctx context.Context, logger zerolog.Logger) *Network {
	return &Network{
		ctx:    ctx,
		logger: logger.With().Str("component", "tss_network").Logger(),
	}
}

// UniversalValidator represents a Universal Validator for network purposes
type UniversalValidator struct {
	ValidatorAddress string // Core validator address
	NetworkIP        string // IP address or domain name (from NetworkInfo)
}

// Initialize sets up the libp2p host
func (n *Network) Initialize(currentUVs []*UniversalValidator) error {
	// TODO: Initialize libp2p network
	// 1. Create libp2p host with listen address, identity, transport, security, muxer
	// 2. Set up stream handlers for TSS protocol messages

	return nil
}

// Refresh refreshes network connections based on current UV set
// Adds connections for new UVs and removes connections for UVs that left or became inactive
func (n *Network) Refresh(currentUVs []*UniversalValidator) error {
	// TODO: Refresh network connections dynamically
	// 1. Filter to only non-inactive UVs
	// 2. Get current connections
	// 3. For each UV in currentUVs:
	//    - If not connected and not self, establish connection
	// 4. For each current connection:
	//    - If UV not in currentUVs or became inactive, close connection

	return nil
}

// Stop stops the network and closes all connections
func (n *Network) Stop() error {
	// TODO: Close all connections and libp2p host
	return nil
}
