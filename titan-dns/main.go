package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/miekg/dns"
)

type DNSHandler struct {
	config   *Config
	cache    *StickyCache
	balancer *LoadBalancer
}

func (h *DNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	msg := dns.Msg{}
	msg.SetReply(r)
	msg.Authoritative = true

	found := false
	for _, q := range r.Question {
		if q.Qtype == dns.TypeA {
			ip := h.resolveSubdomain(q.Name)
			if ip != "" {
				ans := &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(h.config.Server.TTLSeconds)},
					A:   net.ParseIP(ip),
				}
				msg.Answer = append(msg.Answer, ans)
				found = true
				log.Printf("Resolved: %s -> %s", q.Name, ip)
			}
		}
	}

	if !found {
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

func (h *DNSHandler) handleAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		PopID string   `json:"pop_id"`
		IPs   []string `json:"ips"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	h.balancer.UpdatePopIPs(req.PopID, req.IPs)
	log.Printf("Updated POP %s with new IPs: %v", req.PopID, req.IPs)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
		config:   cfg,
		cache:    NewStickyCache(cfg.Server.TTLSeconds),
		balancer: NewLoadBalancer(cfg.Pops),
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
