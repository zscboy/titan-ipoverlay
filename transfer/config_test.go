package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadConfigBackendAddress(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		want    BackendInfo
		wantErr bool
	}{
		{name: "http IPv4", backend: "http://127.0.0.1:8080", want: BackendInfo{Type: "http", Addr: "127.0.0.1:8080"}},
		{name: "socks5 hostname", backend: "socks5://pop.example:1080", want: BackendInfo{Type: "socks5", Addr: "pop.example:1080"}},
		{name: "IPv6", backend: "http://[::1]:8080", want: BackendInfo{Type: "http", Addr: "[::1]:8080"}},
		{name: "minimum port", backend: "http://localhost:1", want: BackendInfo{Type: "http", Addr: "localhost:1"}},
		{name: "maximum port", backend: "socks5://localhost:65535", want: BackendInfo{Type: "socks5", Addr: "localhost:65535"}},
		{name: "original invalid port", backend: "http://127.0.0.1:80801", wantErr: true},
		{name: "port overflow", backend: "socks5://localhost:65536", wantErr: true},
		{name: "zero port", backend: "http://localhost:0", wantErr: true},
		{name: "negative port", backend: "http://localhost:-1", wantErr: true},
		{name: "non-numeric port", backend: "http://localhost:http", wantErr: true},
		{name: "missing port", backend: "http://localhost", wantErr: true},
		{name: "empty port", backend: "http://localhost:", wantErr: true},
		{name: "missing host", backend: "http://:8080", wantErr: true},
		{name: "empty address", backend: "http://", wantErr: true},
		{name: "unbracketed IPv6", backend: "http://::1:8080", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(Config{
				HTTPListen:  "127.0.0.1:0",
				BackendsRaw: []string{tt.backend},
			})
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("LoadConfig accepted invalid backend %q", tt.backend)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Backends) != 1 || cfg.Backends[0] != tt.want {
				t.Fatalf("backends = %+v, want [%+v]", cfg.Backends, tt.want)
			}
		})
	}
}

func TestLoadConfigRouterCleanupInterval(t *testing.T) {
	for _, tt := range []struct {
		value   string
		want    time.Duration
		wantErr bool
	}{
		{value: "", want: 30 * time.Minute},
		{value: "30m", want: 30 * time.Minute},
		{value: "45m", want: 45 * time.Minute},
		{value: "1h", want: time.Hour},
		{value: "0s", wantErr: true},
		{value: "-1m", wantErr: true},
		{value: "invalid", wantErr: true},
	} {
		t.Run(tt.value, func(t *testing.T) {
			config := map[string]any{
				"http_listen": "127.0.0.1:0",
				"backends":    []string{"http://localhost:8080"},
			}
			if tt.value != "" {
				config["router_cleanup_interval"] = tt.value
			}
			data, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			cfg, err := LoadConfig(path)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("accepted invalid cleanup interval %q", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.CleanupIntervalDuration != tt.want {
				t.Fatalf("cleanup interval = %v, want %v", cfg.CleanupIntervalDuration, tt.want)
			}
		})
	}
}
