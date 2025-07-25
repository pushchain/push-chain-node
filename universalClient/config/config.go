package config

type Config struct {
	LogLevel   int    // e.g., 0 = debug, 1 = info, etc.
	LogFormat  string // "json" or "console"
	LogSampler bool   // if true, samples logs (e.g., 1 in 5)
}
