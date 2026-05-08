package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/miekg/dns"
)

type DNSHandler struct {
	config     *Config
	configPath string
	mu         sync.RWMutex // Changed to RWMutex for better concurrency
	cache      *StickyCache
	balancer   *LoadBalancer
	stats      sync.Map // domain -> *int64
}

func (h *DNSHandler) ServeDNS(w dns.ResponseWriter, r *dns.Msg) {
	h.mu.RLock()
	defer h.mu.RUnlock()

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
				// Count query internally instead of printing every time
				h.incStat(q.Name, ip)
			}
		} else {
			// For non-A queries, check if the domain exists to decide between NOERROR and NXDOMAIN.
			// Use a non-destructive check that doesn't trigger the load balancer counter.
			if h.isDomainManaged(q.Name) {
				found = true
				// log.Printf("Domain exists but type %d not handled: %s", q.Qtype, q.Name)
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
		http.HandleFunc("/api/v1/reload", h.handleReloadAPI)
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

	// 3. Start statistics loop (prints every 10s and resets)
	go h.printStatsLoop()

	return server.ListenAndServe()
}

func (h *DNSHandler) ReloadConfig() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	cfg, err := LoadConfig(h.configPath)
	if err != nil {
		return err
	}

	h.config = cfg
	h.balancer = NewLoadBalancer(cfg.Pops)
	// We keep the cache but it might contain old entries.
	// For a clean reload, we could h.cache.Clear() if needed.

	log.Printf("Configuration RELOADED from %s", h.configPath)
	return nil
}

func (h *DNSHandler) incStat(name, resolvedIP string) {
	name = strings.ToLower(name)
	suffix := strings.ToLower(h.config.Server.DomainSuffix)
	if !strings.HasSuffix(suffix, ".") {
		suffix += "."
	}

	// Calculate prefix relative to suffix
	prefix := strings.TrimSuffix(name, suffix)
	parts := strings.Split(strings.TrimSuffix(prefix, "."), ".")

	// If prefix has more than 1 part (e.g., sessionID.popID), aggregate it
	// session123.pop1.abc.com -> *.pop1.abc.com
	if len(parts) > 1 {
		// Use strings.TrimLeft to avoid double dots if suffix already starts with a dot
		name = "*." + parts[len(parts)-1] + "." + strings.TrimLeft(suffix, ".")
	}

	statKey := fmt.Sprintf("%s->%s", name, resolvedIP)
	val, _ := h.stats.LoadOrStore(statKey, new(int64))
	atomic.AddInt64(val.(*int64), 1)
}

func (h *DNSHandler) printStatsLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		hasData := false
		h.stats.Range(func(key, value interface{}) bool {
			if !hasData {
				log.Println("=== DNS Stats (Last 10s) ===")
				hasData = true
			}
			count := atomic.SwapInt64(value.(*int64), 0)
			if count > 0 {
				log.Printf("%s: %d", key, count)
			}
			return true
		})
		if hasData {
			// Clear the map to keep memory usage low and remove old entries
			h.stats = sync.Map{}
		}
	}
}
