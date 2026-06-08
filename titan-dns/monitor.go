package main

import (
	"log"
	"net"
	"strconv"
	"sync"
	"time"
)

// IPStatusTracker tracks health check history for a single IP.
type IPStatusTracker struct {
	mu                  sync.Mutex
	consecutiveFailures int
}

// Blacklist maintains a thread-safe map of unreachable IPs.
type Blacklist struct {
	ips sync.Map // map[string]bool
}

func NewBlacklist() *Blacklist {
	return &Blacklist{}
}

func (b *Blacklist) Add(ip string) {
	b.ips.Store(ip, true)
}

func (b *Blacklist) Remove(ip string) {
	b.ips.Delete(ip)
}

func (b *Blacklist) Contains(ip string) bool {
	_, ok := b.ips.Load(ip)
	return ok
}

// TCPMonitor runs a background health check loop for POP IPs.
type TCPMonitor struct {
	getConfig   func() MonitorConfig
	getBalancer func() *LoadBalancer
	blacklist   *Blacklist
	history     sync.Map // Track status histories (ip -> *IPStatusTracker)
}

func NewTCPMonitor(getConfig func() MonitorConfig, getBalancer func() *LoadBalancer, blacklist *Blacklist) *TCPMonitor {
	return &TCPMonitor{
		getConfig:   getConfig,
		getBalancer: getBalancer,
		blacklist:   blacklist,
	}
}

// Start runs the background loop to periodically test IP connectivity.
func (m *TCPMonitor) Start() {
	cfg := m.getConfig()
	if !cfg.Enabled {
		log.Println("[MONITOR] TCP Health check monitor is disabled.")
		return
	}

	log.Printf("[MONITOR] Starting TCP health check monitor. Port: %d, Interval: %ds, Timeout: %ds",
		cfg.Port, cfg.IntervalSeconds, cfg.TimeoutSeconds)

	go func() {
		// Run initial check immediately
		m.checkAllIPs()

		ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			m.checkAllIPs()
		}
	}()
}

// checkAllIPs dials all unique configured POP IPs concurrently.
func (m *TCPMonitor) checkAllIPs() {
	balancer := m.getBalancer()
	if balancer == nil {
		return
	}
	ips := balancer.GetAllUniqueIPs()
	if len(ips) == 0 {
		return
	}

	cfg := m.getConfig()
	port := cfg.Port
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	unhealthyThreshold := cfg.UnhealthyThreshold

	concurrencyLimit := cfg.ConcurrencyLimit
	if concurrencyLimit <= 0 {
		concurrencyLimit = 5
	}
	sem := make(chan struct{}, concurrencyLimit)

	var wg sync.WaitGroup

	for _, ip := range ips {
		sem <- struct{}{} // Acquire semaphore slot
		wg.Add(1)
		go func(targetIP string) {
			defer wg.Done()
			defer func() { <-sem }() // Release semaphore slot
			addr := net.JoinHostPort(targetIP, strconv.Itoa(port))
			conn, err := net.DialTimeout("tcp", addr, timeout)

			// Get or create history tracker for this IP
			val, _ := m.history.LoadOrStore(targetIP, &IPStatusTracker{})
			tracker := val.(*IPStatusTracker)

			tracker.mu.Lock()
			if err != nil {
				tracker.consecutiveFailures++
				if tracker.consecutiveFailures >= unhealthyThreshold {
					if !m.blacklist.Contains(targetIP) {
						log.Printf("[MONITOR] IP %s has failed %d consecutive checks. Adding to blacklist. Error: %v", targetIP, tracker.consecutiveFailures, err)
						m.blacklist.Add(targetIP)
					}
				}
			} else {
				conn.Close()
				tracker.consecutiveFailures = 0
				if m.blacklist.Contains(targetIP) {
					log.Printf("[MONITOR] IP %s is now REACHABLE. Removing from blacklist.", targetIP)
					m.blacklist.Remove(targetIP)
				}
			}
			tracker.mu.Unlock()
		}(ip)
	}
	wg.Wait()
}
