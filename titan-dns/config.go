package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	Pops   []PopConfig  `yaml:"pops"`
}

type ServerConfig struct {
	Listen       string `yaml:"listen"`
	DomainSuffix string `yaml:"domain_suffix"`
	RecordTTL    int    `yaml:"record_ttl"` // TTL sent in DNS responses
	CacheTTL     int    `yaml:"cache_ttl"`  // TTL for internal session cache
	Secret       string `yaml:"secret"`     // Production grade secret for HMAC
}

type PopConfig struct {
	ID     string   `yaml:"id"`
	Name   string   `yaml:"name"` // Human-readable name for identified
	IPs    []string `yaml:"ips"`
	Follow []string `yaml:"follow"` // Track and aggregate IPs from these POP IDs
	Weight int      `yaml:"weight"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
