package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

type BackendInfo struct {
	Type string // "socks5" or "http"
	Addr string // "host:port"
}

type Config struct {
	Socks5Listen          string        `json:"socks5_listen"`             // "host:port", optional
	HTTPListen            string        `json:"http_listen"`               // "host:port", optional
	BackendsRaw           []string      `json:"backends"`                  // list of "socks5://host:port" or "http://host:port"
	SessionTimeout        string        `json:"session_timeout"`           // e.g. "12h"
	ReadLimitBytesPerSec  int64         `json:"read_limit_bytes_per_sec"`  // bytes per sec, 0 means unthrottled
	WriteLimitBytesPerSec int64         `json:"write_limit_bytes_per_sec"` // bytes per sec, 0 means unthrottled
	BurstBytes            int           `json:"burst_bytes"`               // burst size in bytes，1/10 of ReadLimitBytesPerSec and WriteLimitBytesPerSec
	Log                   logx.LogConf  `json:"log"`
	TimeoutDuration       time.Duration `json:"-"`
	Backends              []BackendInfo `json:"-"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	if cfg.Socks5Listen == "" && cfg.HTTPListen == "" {
		return nil, fmt.Errorf("either socks5_listen or http_listen must be configured")
	}

	if len(cfg.BackendsRaw) == 0 {
		return nil, fmt.Errorf("backends list cannot be empty")
	}

	// Parse backends
	for _, raw := range cfg.BackendsRaw {
		u, err := url.Parse(raw)
		if err != nil {
			return nil, fmt.Errorf("failed to parse backend '%s': %v", raw, err)
		}
		if u.Scheme != "socks5" && u.Scheme != "http" {
			return nil, fmt.Errorf("unsupported backend protocol '%s' in '%s', must be 'socks5://' or 'http://'", u.Scheme, raw)
		}
		cfg.Backends = append(cfg.Backends, BackendInfo{
			Type: u.Scheme,
			Addr: u.Host,
		})
	}

	// Parse timeout
	if cfg.SessionTimeout != "" {
		duration, err := time.ParseDuration(cfg.SessionTimeout)
		if err != nil {
			return nil, fmt.Errorf("invalid session_timeout '%s': %v", cfg.SessionTimeout, err)
		}
		cfg.TimeoutDuration = duration
	} else {
		cfg.TimeoutDuration = 12 * time.Hour
	}

	return &cfg, nil
}
