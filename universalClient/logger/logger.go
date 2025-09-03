package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"

	"github.com/pushchain/push-chain-node/universalClient/config"
)

// Init sets up the global zerolog logger based on config.
// Supports console/json format, level filtering, and optional sampling.
func Init(cfg config.Config) zerolog.Logger {
	var writer io.Writer = os.Stdout
	if cfg.LogFormat != "json" {
		writer = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	logger := zerolog.New(writer).
		Level(zerolog.Level(cfg.LogLevel)).
		With().
		Timestamp().
		Logger()

	if cfg.LogSampler {
		logger = logger.Sample(&zerolog.BasicSampler{N: 5})
	}
	return logger
}
