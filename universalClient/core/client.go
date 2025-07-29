package core

import (
	"context"

	"github.com/rollchains/pchain/universalClient/db"
	"github.com/rs/zerolog"
)

type UniversalClient struct {
	ctx context.Context
	log zerolog.Logger
	db  *db.DB
}

func NewUniversalClient(ctx context.Context, log zerolog.Logger, db *db.DB) *UniversalClient {
	return &UniversalClient{
		ctx: ctx,
		log: log,
		db:  db,
	}
}

func (uc *UniversalClient) Start() error {
	uc.log.Info().Msg("🚀 Starting universal client...")
	uc.log.Info().Msg("✅ Initialization complete. Entering main loop...")

	<-uc.ctx.Done()

	uc.log.Info().Msg("🛑 Shutting down universal client...")
	return uc.db.Close()
}
