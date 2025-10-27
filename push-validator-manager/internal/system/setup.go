package system

// Guided system setup for nginx/logrotate. No automatic package installs.
// Implementers should prompt for sudo if needed and generate configs idempotently.

type NginxConfig struct {
    ServerName string
    RPCPort    int
    WS         bool
}

func SetupNginx(cfg NginxConfig) error { return nil }

type LogrotateConfig struct {
    LogPath string
    Rotate  int
    SizeMB  int
}

func SetupLogrotate(cfg LogrotateConfig) error { return nil }

