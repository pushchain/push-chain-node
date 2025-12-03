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
	// Read nodes from shared registry file - already returns UniversalValidator
	nodes, err := readNodeRegistry(p.logger)
	if err != nil {
		return nil, fmt.Errorf("failed to read node registry: %w", err)
	}

	// Ensure all nodes have valid status (default to pending_join if missing)
	for _, node := range nodes {
		if node.Status == coordinator.UVStatusUnspecified || node.Status == "" {
			p.logger.Debug().
				Str("validator", node.ValidatorAddress).
				Msg("node has unspecified status, defaulting to pending_join")
			node.Status = coordinator.UVStatusPendingJoin
		}
	}

	return nodes, nil
}

// GetCurrentTSSKeyId returns the current TSS key ID.
// Checks the latest created keyshare file across all valid nodes.
// New nodes might not have a keyId yet, so we check all nodes and return the latest one found.
func (p *StaticPushChainDataProvider) GetCurrentTSSKeyId(ctx context.Context) (string, error) {
	// Read all nodes from registry
	nodes, err := readNodeRegistry(p.logger)
	if err != nil {
		return "", fmt.Errorf("failed to read node registry: %w", err)
	}

	if len(nodes) == 0 {
		// No nodes found, return empty string
		return "", nil
	}

	// Collect all keyshare files from all nodes
	type fileInfo struct {
		keyID   string
		modTime time.Time
		node    string
	}

	allFiles := make([]fileInfo, 0)

	// Check each node's keyshare directory
	for _, node := range nodes {
		// Construct the keyshare directory path for this node
		sanitized := strings.ReplaceAll(strings.ReplaceAll(node.ValidatorAddress, ":", "_"), "/", "_")
		keyshareDir := filepath.Join("./tss-data", fmt.Sprintf("tss-%s", sanitized), "keyshares")

		// Check if directory exists (new nodes might not have keyshares yet)
		if _, err := os.Stat(keyshareDir); os.IsNotExist(err) {
			p.logger.Debug().
				Str("node", node.ValidatorAddress).
				Str("keyshare_dir", keyshareDir).
				Msg("keyshare directory does not exist for node, skipping")
			continue
		}

		// Read all files in the keyshare directory
		entries, err := os.ReadDir(keyshareDir)
		if err != nil {
			p.logger.Warn().
				Err(err).
				Str("node", node.ValidatorAddress).
				Str("keyshare_dir", keyshareDir).
				Msg("failed to read keyshare directory, skipping")
			continue
		}

		// Get file info for all entries
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				p.logger.Warn().
					Err(err).
					Str("node", node.ValidatorAddress).
					Str("file", entry.Name()).
					Msg("failed to get file info, skipping")
				continue
			}
			allFiles = append(allFiles, fileInfo{
				keyID:   entry.Name(),
				modTime: info.ModTime(),
				node:    node.ValidatorAddress,
			})
		}
	}

	if len(allFiles) == 0 {
		// No keyshares found across all nodes
		return "", nil
	}

	// Sort by modification time (newest first)
	sort.Slice(allFiles, func(i, j int) bool {
		return allFiles[i].modTime.After(allFiles[j].modTime)
	})

	// Return the most recent keyshare file name (which is the keyID)
	latestKeyID := allFiles[0].keyID
	p.logger.Debug().
		Str("key_id", latestKeyID).
		Str("from_node", allFiles[0].node).
		Time("mod_time", allFiles[0].modTime).
		Int("total_keyshares", len(allFiles)).
		Msg("found latest keyId from all nodes")

	return latestKeyID, nil
}
