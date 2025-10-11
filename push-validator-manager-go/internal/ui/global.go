package ui

// Global UI configuration for the application (set once at startup)
var globalConfig = Config{}

// Config holds application-wide UI settings
type Config struct {
	NoColor        bool
	NoEmoji        bool
	Yes            bool
	NonInteractive bool
	Verbose        bool
	Quiet          bool
	Debug          bool
}

// InitGlobal initializes the global UI configuration (call once at startup)
func InitGlobal(cfg Config) {
	globalConfig = cfg
}

// GetGlobal returns the global UI configuration
func GetGlobal() Config {
	return globalConfig
}

// NewColorConfigFromGlobal creates a ColorConfig using global settings
func NewColorConfigFromGlobal() *ColorConfig {
	cfg := GetGlobal()
	c := NewColorConfig()
	c.Enabled = c.Enabled && !cfg.NoColor
	c.EmojiEnabled = c.EmojiEnabled && !cfg.NoEmoji
	return c
}

// NewPrinterFromGlobal creates a Printer using global settings
func NewPrinterFromGlobal(format string) Printer {
	return Printer{
		format: format,
		Colors: NewColorConfigFromGlobal(),
	}
}
