package logger

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/rollchains/pchain/universalClient/config"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestInitVariants(t *testing.T) {
	t.Run("json format logs expected fields", func(t *testing.T) {
		r, w, _ := os.Pipe()
		defer r.Close()

		stdout := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = stdout }()

		logger := Init(config.Config{
			LogFormat:  "json",
			LogLevel:   int(zerolog.InfoLevel),
			LogSampler: false,
		})

		logger.Info().Str("key", "value").Msg("json_test")

		_ = w.Close()
		buf := make([]byte, 1024)
		n, _ := r.Read(buf)

		logOutput := string(buf[:n])
		require.Contains(t, logOutput, `"message":"json_test"`)
		require.Contains(t, logOutput, `"key":"value"`)
	})

	t.Run("console format logs human readable output", func(t *testing.T) {
		r, w, _ := os.Pipe()
		defer r.Close()

		stdout := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = stdout }()

		logger := Init(config.Config{
			LogFormat:  "console",
			LogLevel:   int(zerolog.DebugLevel),
			LogSampler: false,
		})

		logger.Debug().Str("env", "test").Msg("console_log")

		_ = w.Close()
		buf := make([]byte, 1024)
		n, _ := r.Read(buf)

		logOutput := stripANSI(string(buf[:n]))
		require.Contains(t, logOutput, "console_log")
		require.Contains(t, logOutput, "env=test")
	})

	t.Run("sampler reduces output frequency", func(t *testing.T) {
		r, w, _ := os.Pipe()
		defer r.Close()

		stdout := os.Stdout
		os.Stdout = w
		defer func() { os.Stdout = stdout }()

		logger := Init(config.Config{
			LogFormat:  "json",
			LogLevel:   int(zerolog.InfoLevel),
			LogSampler: true,
		})

		for i := 0; i < 20; i++ {
			logger.Info().Int("count", i).Msg("sampled")
		}

		_ = w.Close()
		buf := make([]byte, 4096)
		n, _ := r.Read(buf)

		logOutput := string(buf[:n])
		count := strings.Count(logOutput, "sampled")
		require.Greater(t, count, 0)
		require.Less(t, count, 20)
	})
}

// stripANSI removes ANSI escape sequences (used in console logs)
func stripANSI(input string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(input, "")
}
