package socks5

import (
	"net"
	"sort"
	"sync"
)

// IPBlacklist is process-local and is intentionally not persisted.
type IPBlacklist struct {
	mu  sync.RWMutex
	ips map[string]struct{}
}

func newIPBlacklist() *IPBlacklist { return &IPBlacklist{ips: make(map[string]struct{})} }

func (b *IPBlacklist) Add(ips []net.IP) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ip := range ips {
		if key := canonicalIP(ip); key != "" {
			b.ips[key] = struct{}{}
		}
	}
}

func (b *IPBlacklist) Remove(ips []net.IP) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ip := range ips {
		delete(b.ips, canonicalIP(ip))
	}
}

func (b *IPBlacklist) Contains(ip net.IP) bool {
	key := canonicalIP(ip)
	if key == "" {
		return false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.ips[key]
	return ok
}

func (b *IPBlacklist) List() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	result := make([]string, 0, len(b.ips))
	for ip := range b.ips {
		result = append(result, ip)
	}
	sort.Strings(result)
	return result
}

func canonicalIP(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String()
	}
	if ip16 := ip.To16(); ip16 != nil {
		return ip16.String()
	}
	return ""
}
