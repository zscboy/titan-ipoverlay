package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"
)

type DNSHandler struct {
	config     *Config
	configPath string
	mu         sync.Mutex
	cache      *StickyCache
	balancer   *LoadBalancer
}

func (h *DNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	log.Printf("resolve domain:%v", r.Question)
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true

	found := false
	for _, q := range r.Question {
		// Only TypeA queries trigger resolution and load balancing (RR increment)
		if q.Qtype == dns.TypeA {
			ip := h.resolveSubdomain(q.Name)
			if ip != "" {
				// Domain exists, mark found=true to avoid NXDOMAIN
				found = true
				ans := &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(h.config.Server.RecordTTL)},
					A:   net.ParseIP(ip),
				}
				msg.Answer = append(msg.Answer, ans)
				log.Printf("Resolved A: %s -> %s", q.Name, ip)
			}
		} else {
			// For non-A queries, check if the domain exists to decide between NOERROR and NXDOMAIN.
			// Use a non-destructive check that doesn't trigger the load balancer counter.
			if h.isDomainManaged(q.Name) {
				found = true
				log.Printf("Domain exists but type %d not handled: %s", q.Qtype, q.Name)
			}
		}
	}

	if !found {
		// Return NXDOMAIN only if the domain is not found at all
		msg.SetRcode(r, dns.RcodeNameError) // NXDOMAIN
	}

	w.WriteMsg(&msg)
}

func (h *DNSHandler) resolveSubdomain(name string) string {
	name = strings.ToLower(name)
	// Remove trailing dot if present
	name = strings.TrimSuffix(name, ".")

	suffix := strings.ToLower(h.config.Server.DomainSuffix)
	if !strings.HasSuffix(name, suffix) {
		return ""
	}

	// 1. First, check if the full name is exactly a configured POP ID (Direct Fixed Domain)
	// Example: pop1.pop.abc.aa.com
	if ip := h.balancer.BalanceByRR(name); ip != "" {
		return ip
	}

	// 2. If not a direct match, check if it's a session subdomain
	// Example: xxx.pop1.pop.abc.aa.com
	// We extract popID (the part after the first dot)
	parts := strings.SplitN(name, ".", 2)
	if len(parts) < 2 {
		return ""
	}

	sessionID := parts[0]
	popID := parts[1] // This will be pop1.pop.abc.aa.com

	// Try Sticky Hash Balance for the Session
	// Check cache first for performance
	if ip, ok := h.cache.Get(name); ok {
		return ip
	}

	ip := h.balancer.BalanceBySession(popID, sessionID)
	if ip != "" {
		h.cache.Set(name, ip)
	}
	return ip
}

// isDomainManaged checks existence without affecting balancing (no RR increment)
func (h *DNSHandler) isDomainManaged(name string) bool {
	name = strings.ToLower(name)
	name = strings.TrimSuffix(name, ".")

	suffix := strings.ToLower(h.config.Server.DomainSuffix)
	if !strings.HasSuffix(name, suffix) {
		return false
	}

	// 1. Direct POP ID match?
	if h.balancer.HasPop(name) {
		return true
	}

	// 2. Session subdomain match?
	parts := strings.SplitN(name, ".", 2)
	if len(parts) < 2 {
		return false
	}

	popID := parts[1]
	return h.balancer.HasPop(popID)
}

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

func (h *DNSHandler) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body to verify signature
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

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
	apiAddr := flag.String("api", ":8080", "address for HTTP API") // Added API listener
	flag.Parse()

	cfg, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Error loading config from %s: %v", *configPath, err)
	}

	handler := &DNSHandler{
		config:     cfg,
		configPath: *configPath,
		cache:      NewStickyCache(cfg.Server.CacheTTL),
		balancer:   NewLoadBalancer(cfg.Pops),
	}

	// Start HTTP API in background
	go func() {
		http.HandleFunc("/api/v1/pop", handler.handleAPI)
		log.Printf("Starting Management API on %s", *apiAddr)
		http.ListenAndServe(*apiAddr, nil)
	}()

	server := &dns.Server{
		Addr:    cfg.Server.Listen,
		Net:     "udp",
		Handler: handler,
	}

	log.Printf("Starting Titan DNS Server on %s", cfg.Server.Listen)
	log.Printf("Handling domains ending in %s", cfg.Server.DomainSuffix)

	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Failed to start server: %s", err.Error())
	}
}
