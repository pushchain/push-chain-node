package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/tss/coordinator"
)

// StaticPushChainDataProvider implements PushChainDataProvider for demo/testing.
// It reads validator information from the shared node registry file.
type StaticPushChainDataProvider struct {
	validatorAddress string
	logger           zerolog.Logger
}

// NewStaticPushChainDataProvider creates a new static data provider.
func NewStaticPushChainDataProvider(validatorAddress string, logger zerolog.Logger) *StaticPushChainDataProvider {
	return &StaticPushChainDataProvider{
		validatorAddress: validatorAddress,
		logger:           logger,
	}
}

// GetLatestBlockNum returns the latest block number.
// For demo purposes, we return current time + 11 to ensure events created with current time
// are immediately eligible for processing (GetPendingEvents requires events to be 10 blocks behind).
func (p *StaticPushChainDataProvider) GetLatestBlockNum(ctx context.Context) (uint64, error) {
	return uint64(time.Now().Unix()) + 11, nil
}

// GetUniversalValidators returns all universal validators.
func (p *StaticPushChainDataProvider) GetUniversalValidators(ctx context.Context) ([]*coordinator.UniversalValidator, error) {
	// Read nodes from shared registry file
	nodes, err := readNodeRegistry(p.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to read node registry: %w", err)
	}

	// Convert to UniversalValidator list
	validators := make([]*coordinator.UniversalValidator, 0, len(nodes))
	for _, node := range nodes {
		validators = append(validators, &coordinator.UniversalValidator{
			ValidatorAddress: node.ValidatorAddress,
			Status:           coordinator.UVStatusActive,
			Network: coordinator.NetworkInfo{
				PeerID:     node.PeerID,
				Multiaddrs: node.Multiaddrs,
			},
			JoinedAtBlock: 0,
		})
	}

	return validators, nil
}

// GetCurrentTSSKeyId returns the current TSS key ID.
// Checks the latest created keyshare file in the tmp directory.
func (p *StaticPushChainDataProvider) GetCurrentTSSKeyId(ctx context.Context) (string, error) {
	// Construct the keyshare directory path based on validator address
	// Default location is /tmp/tss-<validator>/keyshares
	sanitized := strings.ReplaceAll(strings.ReplaceAll(p.validatorAddress, ":", "_"), "/", "_")
	keyshareDir := filepath.Join("/tmp", fmt.Sprintf("tss-%s", sanitized), "keyshares")

	// Check if directory exists
	if _, err := os.Stat(keyshareDir); os.IsNotExist(err) {
		// No keyshares directory yet, return empty string
		return "", nil
	}

	// Read all files in the keyshare directory
	entries, err := os.ReadDir(keyshareDir)
	if err != nil {
		return "", fmt.Errorf("failed to read keyshare directory: %w", err)
	}

	if len(entries) == 0 {
		// No keyshares found
		return "", nil
	}

	// Get file info for all entries and sort by modification time
	type fileInfo struct {
		name    string
		modTime time.Time
	}

	files := make([]fileInfo, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			p.logger.Warn().Err(err).Str("file", entry.Name()).Msg("failed to get file info, skipping")
			continue
		}
		files = append(files, fileInfo{
			name:    entry.Name(),
			modTime: info.ModTime(),
		})
	}

	if len(files) == 0 {
		return "", nil
	}

	// Sort by modification time (newest first)
	sort.Slice(files, func(i, j int) bool {
		return files[i].modTime.After(files[j].modTime)
	})

	// Return the most recent keyshare file name (which is the keyID)
	return files[0].name, nil
}
