package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"
)

func (h *DNSHandler) validateSignature(r *http.Request, body []byte) bool {
	secret := h.config.Server.Secret
	if secret == "" {
		return true // Allow if no secret configured
	}

	timestamp := r.Header.Get("X-Titan-Timestamp")
	signature := r.Header.Get("X-Titan-Signature")

	if timestamp == "" || signature == "" {
		return false
	}

	// 1. Check timestamp (5 mins window)
	t, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	if math.Abs(float64(time.Now().Unix()-t)) > 300 {
		return false
	}

	// 2. Calculate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	mac.Write([]byte(timestamp))
	expectedMac := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMac))
}

func (h *DNSHandler) handlePopIPsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request", http.StatusInternalServerError)
		return
	}

	if !h.validateSignature(r, body) {
		http.Error(w, "Forbidden: Invalid Signature", http.StatusForbidden)
		log.Printf("Forbidden access attempt (invalid signature) from %s", r.RemoteAddr)
		return
	}

	var req struct {
		PopID string   `json:"pop_id"`
		IPs   []string `json:"ips"`
	}

	if err := json.Unmarshal(body, &req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// 1. Update In-Memory Balancer (Hot update)
	h.balancer.UpdatePopIPs(req.PopID, req.IPs)

	// 2. Update In-Memory Config Structure
	updated := false
	for i, p := range h.config.Pops {
		if p.ID == req.PopID {
			h.config.Pops[i].IPs = req.IPs
			updated = true
			break
		}
	}
	// If it's a new POPID not in config, add it as a new entry
	if !updated {
		h.config.Pops = append(h.config.Pops, PopConfig{
			ID:   req.PopID,
			Name: "Dynamic-Added-" + req.PopID,
			IPs:  req.IPs,
		})
	}

	// 3. Persist to Disk
	if err := SaveConfig(h.configPath, h.config); err != nil {
		log.Printf("ERROR saving config to %s: %v", h.configPath, err)
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Invalidate Cache for this POP to ensure new IP list is used immediately
	h.cache.RemoveByPop(req.PopID)

	log.Printf("Successfully updated, PERSISTED and CACHE-CLEARED POP %s with new IPs: %v", req.PopID, req.IPs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "updated and persisted"})
}

func (h *DNSHandler) handleFollowAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request", http.StatusInternalServerError)
		return
	}

	if !h.validateSignature(r, body) {
		http.Error(w, "Forbidden: Invalid Signature", http.StatusForbidden)
		log.Printf("Forbidden follow-update attempt (invalid signature) from %s", r.RemoteAddr)
		return
	}

	var req struct {
		PopID  string   `json:"pop_id"`
		Follow []string `json:"follow"`
	}

	if err := json.NewDecoder(bytes.NewBuffer(body)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	// 1. Update In-Memory Balancer
	h.balancer.UpdatePopFollows(req.PopID, req.Follow)

	// 2. Update In-Memory Config Structure
	updated := false
	for i, p := range h.config.Pops {
		if p.ID == req.PopID {
			h.config.Pops[i].Follow = req.Follow
			updated = true
			break
		}
	}
	if !updated {
		h.config.Pops = append(h.config.Pops, PopConfig{
			ID:     req.PopID,
			Name:   "Dynamic-Follow-" + req.PopID,
			Follow: req.Follow,
		})
	}

	// 3. Persist to Disk
	if err := SaveConfig(h.configPath, h.config); err != nil {
		log.Printf("ERROR saving config to %s: %v", h.configPath, err)
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// 4. Invalidate Cache
	h.cache.RemoveByPop(req.PopID)

	log.Printf("Successfully updated FOLLOW for POP %s: %v", req.PopID, req.Follow)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "message": "follow updated and persisted"})
}
