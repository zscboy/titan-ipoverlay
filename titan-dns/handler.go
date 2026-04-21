package main

import (
	"log"
	"net"
	"net/http"
	"strings"
	"sync"

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

func NewDNSHandler(cfg *Config, path string) *DNSHandler {
	return &DNSHandler{
		config:     cfg,
		configPath: path,
		cache:      NewStickyCache(cfg.Server.CacheTTL),
		balancer:   NewLoadBalancer(cfg.Pops),
	}
}

func (h *DNSHandler) Start(apiAddr string) error {
	// 1. Start HTTP API
	go func() {
		http.HandleFunc("/api/v1/pop", h.handlePopIPsAPI)
		http.HandleFunc("/api/v1/follow", h.handleFollowAPI)
		log.Printf("Starting Management API on %s", apiAddr)
		if err := http.ListenAndServe(apiAddr, nil); err != nil {
			log.Fatalf("HTTP API failed: %v", err)
		}
	}()

	// 2. Start DNS Server
	server := &dns.Server{
		Addr:    h.config.Server.Listen,
		Net:     "udp",
		Handler: h,
	}

	log.Printf("Starting Titan DNS Server on %s", h.config.Server.Listen)
	log.Printf("Handling domains ending in %s", h.config.Server.DomainSuffix)
	return server.ListenAndServe()
}
