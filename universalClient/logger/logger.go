package logger

import (
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// New creates a new zerolog logger with the specified configuration.
// Supports console/json format, level filtering, and optional sampling.
func New(logLevel int, logFormat string, logSampler bool) zerolog.Logger {
	var writer io.Writer = os.Stdout
	if logFormat != "json" {
		writer = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	logger := zerolog.New(writer).
		Level(zerolog.Level(logLevel)).
		With().
		Timestamp().
		Logger()

	if logSampler {
		logger = logger.Sample(&zerolog.BasicSampler{N: 5})
	}
	return logger
}
