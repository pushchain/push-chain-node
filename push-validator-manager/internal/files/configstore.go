package files

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ConfigStore abstracts config/app.toml and related files with idempotent writes.
type ConfigStore interface {
	EnableStateSync(params StateSyncParams) error
	DisableStateSync() error
	SetPersistentPeers(peers []string) error
	Backup() (string, error) // returns backup path of config.toml
}

type StateSyncParams struct {
	TrustHeight         int64
	TrustHash           string
	RPCServers          []string // full URLs, comma separated when rendered
	TrustPeriod         string   // e.g., 336h0m0s
	ChunkFetchers       int      // parallel chunk downloads (default: 12)
	ChunkRequestTimeout string   // timeout for chunk requests (default: 15m0s)
	DiscoveryTime       string   // time to discover snapshots (default: 90s)
}

type store struct{ home string }

// New returns a filesystem-backed store rooted at home.
func New(home string) ConfigStore { return &store{home: home} }

func (s *store) cfgPath() string { return filepath.Join(s.home, "config", "config.toml") }

func (s *store) readConfig() (string, error) {
	b, err := os.ReadFile(s.cfgPath())
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (s *store) writeConfig(content string) error {
	return os.WriteFile(s.cfgPath(), []byte(content), 0o644)
}

func (s *store) Backup() (string, error) {
	src := s.cfgPath()
	ts := time.Now().Format("20060102-150405")
	dst := src + "." + ts + ".bak"
	b, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, b, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func (s *store) EnableStateSync(params StateSyncParams) error {
	content, err := s.readConfig()
	if err != nil {
		return err
	}
	// Ensure [statesync] section
	if !regexp.MustCompile(`(?m)^\[statesync\]\s*$`).MatchString(content) {
		content += "\n[statesync]\n"
	}
	content = setInSection(content, "statesync", map[string]string{
		"enable":                "true",
		"rpc_servers":           fmt.Sprintf("\"%s\"", strings.Join(params.RPCServers, ",")),
		"trust_height":          fmt.Sprintf("%d", params.TrustHeight),
		"trust_hash":            fmt.Sprintf("\"%s\"", params.TrustHash),
		"trust_period":          fmt.Sprintf("\"%s\"", valueOrDefault(params.TrustPeriod, "336h0m0s")),
		"chunk_fetchers":        fmt.Sprintf("%d", valueOrDefaultInt(params.ChunkFetchers, 12)),
		"chunk_request_timeout": fmt.Sprintf("\"%s\"", valueOrDefault(params.ChunkRequestTimeout, "15m0s")),
		"discovery_time":        fmt.Sprintf("\"%s\"", valueOrDefault(params.DiscoveryTime, "90s")),
	})
	return s.writeConfig(content)
}

func (s *store) DisableStateSync() error {
	content, err := s.readConfig()
	if err != nil {
		return err
	}
	content = setInSection(content, "statesync", map[string]string{
		"enable": "false",
	})
	return s.writeConfig(content)
}

func (s *store) SetPersistentPeers(peers []string) error {
	content, err := s.readConfig()
	if err != nil {
		return err
	}
	if !regexp.MustCompile(`(?m)^\[p2p\]\s*$`).MatchString(content) {
		content += "\n[p2p]\n"
	}
	content = setInSection(content, "p2p", map[string]string{
		"persistent_peers": fmt.Sprintf("\"%s\"", strings.Join(peers, ",")),
		"addr_book_strict": "false",
		"pex":              "false",
	})
	return s.writeConfig(content)
}

func setInSection(content, section string, kv map[string]string) string {
	// Locate section bounds
	reStart := regexp.MustCompile("(?m)^\\[" + regexp.QuoteMeta(section) + "\\]\\s*$")
	loc := reStart.FindStringIndex(content)
	if loc == nil {
		return content
	}
	start := loc[1]
	// Find next section
	reAny := regexp.MustCompile("(?m)^\\[[^]]+\\]\\s*$")
	next := reAny.FindStringIndex(content[start:])
	end := len(content)
	if next != nil {
		end = start + next[0]
	}
	before := content[:start]
	block := content[start:end]
	after := content[end:]
	// Apply/replace keys within block
	for k, v := range kv {
		re := regexp.MustCompile("(?m)^\\s*" + regexp.QuoteMeta(k) + "\\s*=\\s*.*$")
		line := fmt.Sprintf("%s = %s", k, v)
		if re.MatchString(block) {
			block = re.ReplaceAllString(block, line)
		} else {
			if len(strings.TrimSpace(block)) > 0 && !strings.HasSuffix(block, "\n") {
				block += "\n"
			}
			block += line + "\n"
		}
	}
	return before + block + after
}

func valueOrDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func valueOrDefaultInt(v, d int) int {
	if v == 0 {
		return d
	}
	return v
}
