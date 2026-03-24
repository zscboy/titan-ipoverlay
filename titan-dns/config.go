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
	TTLSeconds   int    `yaml:"ttl_seconds"`
	Secret       string `yaml:"secret"` // Production grade secret for HMAC
}

type PopConfig struct {
	ID     string   `yaml:"id"`
	Name   string   `yaml:"name"` // Human-readable name for identified
	IPs    []string `yaml:"ips"`
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
