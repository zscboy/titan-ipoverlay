package main

import (
	"fmt"
	"net/url"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// CellInfo holds metadata and target forwarding URL for a Cell.
type CellInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name,optional"`
	TargetURL string `json:"target_url"`
}

// L1CacheConfig holds configuration for local in-memory L1 cache.
type L1CacheConfig struct {
	Enabled    bool `json:"enabled,default=true"`
	TTLSeconds int  `json:"ttl_seconds,default=86400"`
	Capacity   int  `json:"capacity,default=1000000"`
}

// SecurityConfig holds rate limiting and security settings.
type SecurityConfig struct {
	RateLimitPerIPSec int `json:"rate_limit_per_ip_sec,default=5"` // Rate limit per IP (req/sec), 0 = disabled
}

// HTTPProxyConfig holds connection pool settings for HTTP reverse proxy.
type HTTPProxyConfig struct {
	MaxIdleConns        int `json:"max_idle_conns,default=50000"`          // Total max idle connections across all hosts
	MaxIdleConnsPerHost int `json:"max_idle_conns_per_host,default=10000"` // Max idle connections per host
	IdleConnTimeoutSec  int `json:"idle_conn_timeout_sec,default=90"`      // Idle connection timeout in seconds
	DialTimeoutSec      int `json:"dial_timeout_sec,default=5"`            // Dial timeout in seconds
}

// Config represents the application configuration.
type Config struct {
	ListenAddr    string              `json:"listen_addr,default=0.0.0.0:8880"`
	Redis         redis.RedisConf     `json:"redis"`
	L1Cache       L1CacheConfig       `json:"l1_cache"`
	Security      SecurityConfig      `json:"security"`
	HTTPProxy     HTTPProxyConfig     `json:"http_proxy"`
	Log           logx.LogConf        `json:"log"`
	DefaultCellID string              `json:"default_cell_id,optional"`
	Cells         map[string]CellInfo `json:"cells"`
}

// LoadConfig reads and parses the configuration file using go-zero conf.Load and validates parameters.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if err := conf.Load(path, &cfg); err != nil {
		return nil, fmt.Errorf("load config error from path '%s': %w", path, err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration in '%s': %w", path, err)
	}

	return &cfg, nil
}

// Validate checks configuration sanity and verifies Cell target URLs.
func (c *Config) Validate() error {
	if len(c.Cells) == 0 {
		return fmt.Errorf("'cells' configuration map cannot be empty")
	}

	for id, cell := range c.Cells {
		if cell.ID == "" {
			return fmt.Errorf("cell id in cells map cannot be empty (key: '%s')", id)
		}
		if cell.TargetURL == "" {
			return fmt.Errorf("cell '%s' target_url cannot be empty", id)
		}
		parsedURL, err := url.Parse(cell.TargetURL)
		if err != nil || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return fmt.Errorf("cell '%s' target_url '%s' is invalid (must be a valid http/https URL): %v", id, cell.TargetURL, err)
		}
	}

	if c.DefaultCellID != "" {
		if _, exists := c.Cells[c.DefaultCellID]; !exists {
			return fmt.Errorf("default_cell_id '%s' is not found in configured 'cells' map", c.DefaultCellID)
		}
	}

	return nil
}
