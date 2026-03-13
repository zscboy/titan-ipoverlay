package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server ServerConfig `yaml:"server"`
	Pops   []PopConfig   `yaml:"pops"`
}

type ServerConfig struct {
	Listen       string `yaml:"listen"`
	DomainSuffix string `yaml:"domain_suffix"`
	TTLSeconds   int    `yaml:"ttl_seconds"`
}

type PopConfig struct {
	IP     string `yaml:"ip"`
	ID     string `yaml:"id"`
	Weight int    `yaml:"weight"`
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
