package core

import (
	"context"
	"fmt"
	"path/filepath"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/pushchain/push-chain-node/universalClient/api"
	"github.com/pushchain/push-chain-node/universalClient/chains"
	"github.com/pushchain/push-chain-node/universalClient/config"
	"github.com/pushchain/push-chain-node/universalClient/constant"
	"github.com/pushchain/push-chain-node/universalClient/db"
	"github.com/pushchain/push-chain-node/universalClient/logger"
	"github.com/pushchain/push-chain-node/universalClient/pushcore"
	"github.com/pushchain/push-chain-node/universalClient/pushsigner"
	"github.com/pushchain/push-chain-node/universalClient/tss"
	"github.com/rs/zerolog"
)

type UniversalClient struct {
	ctx         context.Context
	log         zerolog.Logger
	config      *config.Config
	queryServer *api.Server
	pushCore    *pushcore.Client
	pushSigner  *pushsigner.Signer
	chains      *chains.Chains
	tssNode     *tss.Node
}

func NewUniversalClient(ctx context.Context, cfg *config.Config) (*UniversalClient, error) {
	if cfg == nil {
		return nil, fmt.Errorf("Config is nil")
	}

	// Initialize logger
	log := logger.New(cfg.LogLevel, cfg.LogFormat, cfg.LogSampler)

	// Initialize pushCore client
	pushCore, err := pushcore.New(cfg.PushChainGRPCURLs, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create pushcore client: %w", err)
	}

	// Convert valoper address to account address for grant validation
	var granterAddr string
	if cfg.PushValoperAddress != "" {
		valAddr, err := sdk.ValAddressFromBech32(cfg.PushValoperAddress)
		if err != nil {
			return nil, fmt.Errorf("failed to parse valoper address %s: %w", cfg.PushValoperAddress, err)
		}
		// Convert validator address to account address (they share the same bytes)
		accAddr := sdk.AccAddress(valAddr)
		granterAddr = accAddr.String()
	}

	// Initialize pushSigner (includes key and AuthZ validation)
	// Grant validation will check grants against the granter address derived from valoper
	pushSigner, err := pushsigner.New(
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

	// Initialize chains manager (fetches chain configs periodically and manages chain clients)
	chainsManager := chains.NewChains(
		pushCore,
		pushSigner,
		cfg,
		log,
	)

	// Initialize TSS node
	var tssNode *tss.Node
	if cfg.PushValoperAddress != "" && cfg.TSSP2PPrivateKeyHex != "" {
		log.Info().Msg("🔑 Initializing TSS node...")

		// Create push chain database
		// Use the same approach as chains manager
		sanitizedChainID := cfg.PushChainID
		// Replace colons and other special chars with underscores for filename
		dbFilename := sanitizedChainID + ".db"
		baseDir := filepath.Join(cfg.NodeHome, constant.DatabasesSubdir)
		pushDB, err := db.OpenFileDB(baseDir, dbFilename, true)
		if err != nil {
			return nil, fmt.Errorf("failed to create Push database: %w", err)
		}

		tssCfg := tss.Config{
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
		}

		tssNode, err = tss.NewNode(ctx, tssCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create TSS node: %w", err)
		}

		log.Info().
			Str("valoper", cfg.PushValoperAddress).
			Str("p2p_listen", cfg.TSSP2PListen).
			Msg("✅ TSS node initialized")
	}

	// Initialize query server
	queryServer := api.NewServer(log, cfg.QueryServerPort)

	// Create and return UniversalClient with all components initialized
	return &UniversalClient{
		ctx:         ctx,
		log:         log,
		config:      cfg,
		queryServer: queryServer,
		pushCore:    pushCore,
		pushSigner:  pushSigner,
		chains:      chainsManager,
		tssNode:     tssNode,
	}, nil
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")

	// Start chains manager (fetches chain configs periodically and manages chain clients)
	if uc.chains != nil {
		if err := uc.chains.Start(uc.ctx); err != nil {
			uc.log.Error().Err(err).Msg("failed to start chains manager")
		} else {
			uc.log.Info().Msg("✅ Chains manager started")
		}
	}

	// Start the TSS node if enabled
	if uc.tssNode != nil {
		if err := uc.tssNode.Start(uc.ctx); err != nil {
			uc.log.Error().Err(err).Msg("failed to start TSS node")
			// Don't fail startup, TSS can recover
		} else {
			uc.log.Info().
				Str("peer_id", uc.tssNode.PeerID()).
				Strs("listen_addrs", uc.tssNode.ListenAddrs()).
				Msg("✅ TSS node started")
		}
	}

	// Start the query server
	if uc.queryServer != nil {
		uc.log.Info().Int("port", uc.config.QueryServerPort).Msg("Starting query server...")
		if err := uc.queryServer.Start(); err != nil {
			return fmt.Errorf("failed to start query server: %w", err)
		}
	} else {
		uc.log.Warn().Msg("Query server is nil, skipping start")
	}

	uc.log.Info().Msg("✅ Initialization complete. Entering main loop...")

	<-uc.ctx.Done()

	uc.log.Info().Msg("🛑 Shutting down universal client...")

	// Stop query server
	if err := uc.queryServer.Stop(); err != nil {
		uc.log.Error().Err(err).Msg("error stopping query server")
	}

	// Stop TSS node
	if uc.tssNode != nil {
		if err := uc.tssNode.Stop(); err != nil {
			uc.log.Error().Err(err).Msg("error stopping TSS node")
		} else {
			uc.log.Info().Msg("✅ TSS node stopped")
		}
	}

	// Stop chains manager (stops all chains and closes databases)
	if uc.chains != nil {
		uc.chains.Stop()
	}

	// Close pushcore client
	if uc.pushCore != nil {
		if err := uc.pushCore.Close(); err != nil {
			uc.log.Error().Err(err).Msg("error closing pushcore client")
		}
	}

	return nil
}
