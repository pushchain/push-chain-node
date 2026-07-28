// Package core provides the top-level orchestrator for the Push Universal Validator.
// It wires together all subsystems (pushcore, pushsigner, externalchains, pushwatcher, tss, api)
// and manages their lifecycle.
package core

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/universalClient/api"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/externalchains"
	"github.com/pushchain/push-chain-node/universalClient/logger"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/pushwatcher"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/rs/zerolog"
)

// UniversalClient is the top-level orchestrator that owns all subsystems.
type UniversalClient struct {
	ctx         context.Context
	log         zerolog.Logger
	config      *config.Config
	queryServer *api.Server
	pushCore    *pushcore.Client
	pushSigner  *pushsigner.Signer
	chains      *externalchains.Chains
	pushChain   *pushwatcher.Client
	tssNode     *tss.Node
}

// NewUniversalClient creates and initializes all subsystems.
// It validates config, connects to Push Chain, sets up signing, chain watchers, and TSS.
func NewUniversalClient(ctx context.Context, cfg *config.Config) (*UniversalClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	log := logger.New(cfg.LogLevel, cfg.LogFormat, cfg.LogSampler)

	pushCore, err := pushcore.New(cfg.PushChainGRPCURLs, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create pushcore client: %w", err)
	}

	granterAddr, err := valoperToAccountAddr(cfg.PushValoperAddress)
	if err != nil {
		return nil, err
	}

	pushSigner, err := pushsigner.New(
		ctx,
		log,
		cfg.KeyringBackend,
		cfg.KeyringPassword,
		cfg.NodeHome,
		pushCore,
		cfg.PushChainID,
		granterAddr,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create pushsigner: %w", err)
	}

	chainsManager := externalchains.NewChains(pushCore, pushSigner, cfg, log)

	// Push chain DB is shared by the push chain client and the TSS node.
	pushDB, err := openPushDB(cfg)
	if err != nil {
		return nil, err
	}

	pushChain, err := pushwatcher.NewClient(
		pushDB,
		cfg.GetChainConfig(cfg.PushChainID),
		pushCore,
		cfg.PushChainID,
		log,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create push chain client: %w", err)
	}

	tssNode, err := initTSS(ctx, cfg, pushCore, chainsManager, pushSigner, pushDB, log)
	if err != nil {
		return nil, err
	}

	queryServer := api.NewServer(log, cfg.QueryServerPort)

	return &UniversalClient{
		ctx:         ctx,
		log:         log.With().Str("component", "core").Logger(),
		config:      cfg,
		queryServer: queryServer,
		pushCore:    pushCore,
		pushSigner:  pushSigner,
		chains:      chainsManager,
		pushChain:   pushChain,
		tssNode:     tssNode,
	}, nil
}

// openPushDB opens the Push chain database under NodeHome.
func openPushDB(cfg *config.Config) (*db.DB, error) {
	if cfg.PushChainID == "" {
		return nil, fmt.Errorf("push_chain_id is required")
	}
	// Sanitize chain ID for use as a database filename (e.g. "push_42101-1" → "push_42101-1.db")
	dbFilename := sanitizeForFilename(cfg.PushChainID) + ".db"
	baseDir := filepath.Join(cfg.NodeHome, config.DatabasesSubdir)
	pushDB, err := db.OpenFileDB(baseDir, dbFilename, true)
	if err != nil {
		return nil, fmt.Errorf("failed to create push database: %w", err)
	}
	return pushDB, nil
}

// Start launches all subsystems, blocks until ctx is cancelled, then shuts down.
func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("starting universal client")

	if err := uc.chains.Start(uc.ctx); err != nil {
		return fmt.Errorf("failed to start chains manager: %w", err)
	}

	if err := uc.pushChain.Start(uc.ctx); err != nil {
		return fmt.Errorf("failed to start push chain client: %w", err)
	}

	if uc.tssNode != nil {
		if err := uc.tssNode.Start(uc.ctx); err != nil {
			return fmt.Errorf("failed to start TSS node: %w", err)
		}
	}

	if err := uc.queryServer.Start(); err != nil {
		return fmt.Errorf("failed to start query server: %w", err)
	}

	uc.log.Info().Msg("universal client running")

	<-uc.ctx.Done()

	uc.shutdown()
	return nil
}

// shutdown stops all subsystems in reverse startup order.
func (uc *UniversalClient) shutdown() {
	uc.log.Debug().Msg("shutting down universal client")

	if err := uc.queryServer.Stop(); err != nil {
		uc.log.Error().Err(err).Str("subsystem", "query_server").Msg("subsystem failed to stop")
	}

	if uc.tssNode != nil {
		if err := uc.tssNode.Stop(); err != nil {
			uc.log.Error().Err(err).Str("subsystem", "tss_node").Msg("subsystem failed to stop")
		}
	}

	if uc.pushChain != nil {
		if err := uc.pushChain.Stop(); err != nil {
			uc.log.Error().Err(err).Str("subsystem", "push_chain").Msg("subsystem failed to stop")
		}
	}

	if uc.chains != nil {
		uc.chains.Stop()
	}

	if uc.pushCore != nil {
		if err := uc.pushCore.Close(); err != nil {
			uc.log.Error().Err(err).Str("subsystem", "push_core").Msg("subsystem failed to close")
		}
	}

	uc.log.Info().Msg("universal client stopped")
}

// valoperToAccountAddr converts a validator operator address to its account address.
func valoperToAccountAddr(valoper string) (string, error) {
	if valoper == "" {
		return "", fmt.Errorf("push_valoper_address is required")
	}
	valAddr, err := sdk.ValAddressFromBech32(valoper)
	if err != nil {
		return "", fmt.Errorf("failed to parse valoper address %s: %w", valoper, err)
	}
	return sdk.AccAddress(valAddr).String(), nil
}

// initTSS creates and returns a TSS node if the config has the required fields.
// Returns nil node (not an error) if TSS config is absent.
func initTSS(
	ctx context.Context,
	cfg *config.Config,
	pushCore *pushcore.Client,
	chainsManager *externalchains.Chains,
	pushSigner *pushsigner.Signer,
	pushDB *db.DB,
	log zerolog.Logger,
) (*tss.Node, error) {
	if cfg.PushValoperAddress == "" || cfg.TSSP2PPrivateKeyHex == "" {
		return nil, nil
	}

	node, err := tss.NewNode(ctx, tss.Config{
		ValidatorAddress: cfg.PushValoperAddress,
		P2PPrivateKeyHex: cfg.TSSP2PPrivateKeyHex,
		LibP2PListen:     cfg.TSSP2PListen,
		HomeDir:          cfg.NodeHome,
		Password:         cfg.TSSPassword,
		Database:         pushDB,
		PushCore:         pushCore,
		Logger:           log,
		Chains:           chainsManager,
		PushSigner:       pushSigner,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create TSS node: %w", err)
	}

	return node, nil
}

// sanitizeForFilename replaces characters that are problematic in filenames.
func sanitizeForFilename(s string) string {
	return strings.ReplaceAll(s, ":", "_")
}
