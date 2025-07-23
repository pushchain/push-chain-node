package core

import (
	"context"
	"log/slog"
)

type UniversalClient struct {
	ctx context.Context
	log *slog.Logger
}

func NewUniversalClient(ctx context.Context, log *slog.Logger) *UniversalClient {
	return &UniversalClient{
		ctx: ctx,
		log: log,
	}
}

func (uc *UniversalClient) Start() error {
	uc.log.Info("🚀 Starting universal client...")
	uc.log.Info("✅ Initialization complete. Entering main loop...")

	// Block forever (or until context is canceled)
	<-uc.ctx.Done()

	uc.log.Info("🛑 Shutting down universal client...")
	return nil
}
