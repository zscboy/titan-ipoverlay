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
	// 1. Build a lookup map for validation
	popMap := make(map[string]struct{}, len(cfg.Pops))
	for _, p := range cfg.Pops {
		popMap[p.ID] = struct{}{}
	}

	// 2. Validate target POP ID
	if _, exists := popMap[popID]; !exists {
		log.Fatalf("Error: POP ID '%s' not found in config file.", popID)
	}

	// 3. Dispatch to specific handlers
	switch cmd {
	case "set-ips":
		handleSetIPs(cfg, apiAddr, popID, dataList)
	case "set-follow":
		handleSetFollow(cfg, apiAddr, popID, dataList, popMap)
	default:
		log.Fatal("Unknown command. Supported: set-ips, set-follow")
	}
}

func handleSetIPs(cfg *Config, apiAddr, popID, ipList string) {
	var ips []string
	if ipList != "" {
		ips = strings.Split(ipList, ",")
	}
	payload := map[string]interface{}{
		"pop_id": popID,
		"ips":    ips,
	}
	sendSignedRequest(apiAddr, "/api/v1/pop", cfg.Server.Secret, payload)
}

func handleSetFollow(cfg *Config, apiAddr, popID, followList string, popMap map[string]struct{}) {
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
