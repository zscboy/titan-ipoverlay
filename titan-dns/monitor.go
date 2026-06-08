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

// OfflineIPs maintains a thread-safe map of unreachable IPs.
type OfflineIPs struct {
	ips sync.Map // map[string]bool
}

func NewOfflineIPs() *OfflineIPs {
	return &OfflineIPs{}
}

func (o *OfflineIPs) Add(ip string) {
	o.ips.Store(ip, true)
}

func (o *OfflineIPs) Remove(ip string) {
	o.ips.Delete(ip)
}

func (o *OfflineIPs) Contains(ip string) bool {
	_, ok := o.ips.Load(ip)
	return ok
}

func (o *OfflineIPs) GetAll() []string {
	var list []string
	o.ips.Range(func(key, value interface{}) bool {
		list = append(list, key.(string))
		return true
	})
	return list
}

func (o *OfflineIPs) Clear() {
	o.ips.Range(func(key, value interface{}) bool {
		o.ips.Delete(key)
		return true
	})
}

// TCPMonitor runs a background health check loop for POP IPs.
type TCPMonitor struct {
	getConfig   func() MonitorConfig
	getBalancer func() *LoadBalancer
	offlineIPs  *OfflineIPs
	history     sync.Map // Track status histories (ip -> *IPStatusTracker)
}

func NewTCPMonitor(getConfig func() MonitorConfig, getBalancer func() *LoadBalancer, offlineIPs *OfflineIPs) *TCPMonitor {
	return &TCPMonitor{
		getConfig:   getConfig,
		getBalancer: getBalancer,
		offlineIPs:  offlineIPs,
	}
}

func (m *TCPMonitor) ClearHistory() {
	m.offlineIPs.Clear()

	m.history.Range(func(key, value interface{}) bool {
		m.history.Delete(key)
		return true
	})
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
					if !m.offlineIPs.Contains(targetIP) {
						log.Printf("[MONITOR] IP %s has failed %d consecutive checks. Marking as offline. Error: %v", targetIP, tracker.consecutiveFailures, err)
						m.offlineIPs.Add(targetIP)
					}
				}
			} else {
				conn.Close()
				tracker.consecutiveFailures = 0
				if m.offlineIPs.Contains(targetIP) {
					log.Printf("[MONITOR] IP %s is now REACHABLE. Marking as online.", targetIP)
					m.offlineIPs.Remove(targetIP)
				}
			}
			tracker.mu.Unlock()
		}(ip)
	}
	wg.Wait()
}
