package main

import (
	"flag"
	"log"
	"net"
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

	for _, q := range r.Question {
		if q.Qtype == dns.TypeA {
			ip := h.resolveSubdomain(q.Name)
			if ip != "" {
				ans := &dns.A{
					Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
					A:   net.ParseIP(ip),
				}
				msg.Answer = append(msg.Answer, ans)
				log.Printf("Resolved: %s -> %s", q.Name, ip)
			}
		}
	}

	w.WriteMsg(&msg)
}

func (h *DNSHandler) resolveSubdomain(name string) string {
	name = strings.ToLower(name)
	suffix := strings.ToLower(h.config.Server.DomainSuffix)
	if !strings.HasSuffix(name, suffix+".") && !strings.HasSuffix(name, suffix) {
		return ""
	}

	// Extract xxx from xxx.abc.aa.com.
	prefix := strings.TrimSuffix(name, suffix+".")
	prefix = strings.TrimSuffix(prefix, ".")
	
	// If the name is exactly the suffix, or has nothing before it, skip.
	if prefix == "" {
		return ""
	}

	// Sticky Cache check
	if ip, ok := h.cache.Get(prefix); ok {
		return ip
	}

	// If not in cache or expired, pick next via balancer
	ip := h.balancer.NextIP()
	if ip != "" {
		h.cache.Set(prefix, ip)
	}

	return ip
}

func main() {
	configPath := flag.String("c", "config.yaml", "path to config file")
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
