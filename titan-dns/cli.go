package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func handleCLI(cfg *Config, apiAddr, cmd, popID, dataList string) {
	// 1. Dispatch to specific handlers
	switch cmd {
	case "set-ips":
		validatePopID(cfg, popID)
		handleSetIPs(cfg, apiAddr, popID, dataList)
	case "set-follow":
		validatePopID(cfg, popID)
		handleSetFollow(cfg, apiAddr, popID, dataList)
	case "reload":
		handleReload(cfg, apiAddr)
	case "set-log":
		handleSetLog(cfg, apiAddr, dataList)
	case "clear-offline":
		handleClearOffline(cfg, apiAddr)
	default:
		log.Fatal("Unknown command. Supported: set-ips, set-follow, reload, set-log, clear-offline")
	}
}

func validatePopID(cfg *Config, popID string) {
	for _, p := range cfg.Pops {
		if p.ID == popID {
			return
		}
	}
	log.Fatalf("Error: POP ID '%s' not found in config file.", popID)
}

func handleReload(cfg *Config, apiAddr string) {
	sendSignedRequest(apiAddr, "/api/v1/reload", cfg.Server.Secret, map[string]string{
		"action": "reload",
	})
}

func handleSetIPs(cfg *Config, apiAddr, popID, ipList string) {
	log.Printf("CLI: Starting to parse IP list: %q for POP ID: %s", ipList, popID)
	ips := make(map[string]int)
	if ipList != "" {
		for _, ipRaw := range strings.Split(ipList, ",") {
			parts := strings.Split(ipRaw, ":")
			if len(parts) == 2 {
				weight, err := strconv.Atoi(parts[1])
				if err != nil {
					log.Fatalf("CLI: Error: Failed to parse weight %q for IP %q (Error: %v)", parts[1], parts[0], err)
				}
				ips[parts[0]] = weight
				log.Printf("CLI:   Parsed IP %s with weight %d", parts[0], weight)
				continue
			}
			ips[parts[0]] = 1
			log.Printf("CLI:   Parsed IP %s with default weight 1", parts[0])
		}
	}
	log.Printf("CLI: Final parsed IPs payload: %v", ips)
	payload := map[string]interface{}{
		"pop_id": popID,
		"ips":    ips,
	}
	log.Printf("CLI: Sending signed request to update IPs for POP %s...", popID)
	sendSignedRequest(apiAddr, "/api/v1/pop", cfg.Server.Secret, payload)
}

func handleSetFollow(cfg *Config, apiAddr, popID, followList string) {
	// Build pop map for validation
	popMap := make(map[string]struct{}, len(cfg.Pops))
	for _, p := range cfg.Pops {
		popMap[p.ID] = struct{}{}
	}

	var follows []string
	if followList != "" {
		follows = strings.Split(followList, ",")
		for _, f := range follows {
			if _, exists := popMap[f]; !exists {
				log.Fatalf("Error: Follow ID '%s' not found in config.", f)
			}
		}
	}
	payload := map[string]interface{}{
		"pop_id": popID,
		"follow": follows,
	}
	sendSignedRequest(apiAddr, "/api/v1/follow", cfg.Server.Secret, payload)
}

func sendSignedRequest(apiAddr, endpoint, secret string, payload interface{}) {
	body, err := json.Marshal(payload)
	if err != nil {
		log.Fatalf("Failed to marshal payload: %v", err)
	}

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Calculate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(timestamp))
	signature := hex.EncodeToString(mac.Sum(nil))

	// Resolve actual URL
	host, port, err := net.SplitHostPort(apiAddr)
	if err != nil {
		if strings.HasPrefix(apiAddr, ":") {
			host = "127.0.0.1"
			port = apiAddr[1:]
		} else {
			log.Fatalf("Invalid API address: %v", err)
		}
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	url := fmt.Sprintf("http://%s:%s%s", host, port, endpoint)

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(body))
	if err != nil {
		log.Fatalf("Failed to create request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Titan-Timestamp", timestamp)
	req.Header.Set("X-Titan-Signature", signature)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("Error from server (%d): %s", resp.StatusCode, string(respBody))
	}
	fmt.Printf("Success: %s\n", string(respBody))
}

func handleSetLog(cfg *Config, apiAddr, value string) {
	enable := false
	if value == "true" || value == "1" || value == "on" {
		enable = true
	}
	payload := map[string]interface{}{
		"enable": enable,
	}
	sendSignedRequest(apiAddr, "/api/v1/log", cfg.Server.Secret, payload)
}

func handleClearOffline(cfg *Config, apiAddr string) {
	payload := map[string]interface{}{
		"action": "clear",
	}
	sendSignedRequest(apiAddr, "/api/v1/offline/clear", cfg.Server.Secret, payload)
}
