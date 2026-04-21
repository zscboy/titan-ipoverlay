package main

import (
	"flag"
	"log"
)

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	apiAddr := flag.String("api", ":8080", "address for HTTP API")
	mode := flag.String("m", "server", "running mode: 'server' or 'cli'")
	cmd := flag.String("cmd", "", "cli command: 'set-follow' or 'set-ips'. Example: ./titan-dns -m cli -cmd set-ips -pop pop1 -val 192.168.1.1,192.168.1.2")
	popID := flag.String("pop", "", "target POP ID")
	valList := flag.String("val", "", "comma separated list of values (IPs or POP IDs)")
	flag.Parse()

	// 1. Load Configuration
	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Fatal: Failed to load config from %s: %v", *configPath, err)
	}

	// 2. Dispatch between CLI and Server mode
	if *mode == "cli" {
		handleCLI(cfg, *apiAddr, *cmd, *popID, *valList)
		return
	}

	// 3. Start DNS and API Server
	handler := NewDNSHandler(cfg, *configPath)
	if err := handler.Start(*apiAddr); err != nil {
		log.Fatalf("Fatal: Server failed: %v", err)
	}
}
