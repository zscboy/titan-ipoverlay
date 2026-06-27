package ws

import (
	"context"
	"fmt"
	"testing"
	"titan-ipoverlay/ippop/model"
)

func TestIPPoolRegionAllocation(t *testing.T) {
	pool := NewIPPool()

	// 1. Create a dummy tunnel with US region
	tunUS := &Tunnel{
		opts: &TunOptions{
			Id:      "node-us-1",
			IP:      "1.1.1.1",
			LocalIP: "192.168.1.1",
			Region:  "us",
		},
	}

	// Create a dummy tunnel with HK region
	tunHK := &Tunnel{
		opts: &TunOptions{
			Id:      "node-hk-1",
			IP:      "2.2.2.2",
			LocalIP: "192.168.1.2",
			Region:  "hk",
		},
	}

	// 2. Add to pool
	pool.AddTunnel(tunUS, false)
	pool.AddTunnel(tunHK, false)

	// 3. Acquire by region
	ip, acquiredTun := pool.AcquireIP("us")
	if acquiredTun == nil || acquiredTun.opts.Id != "node-us-1" {
		t.Fatalf("expected to acquire node-us-1 for region 'us', got IP %s", ip)
	}

	ip, acquiredTun = pool.AcquireIP("hk")
	if acquiredTun == nil || acquiredTun.opts.Id != "node-hk-1" {
		t.Fatalf("expected to acquire node-hk-1 for region 'hk', got IP %s", ip)
	}

	// 4. Try to acquire for empty/non-existent region, should fall back or fail
	ip, acquiredTun = pool.AcquireIP("jp")
	if acquiredTun != nil {
		t.Errorf("expected no tunnel for 'jp' region, got %s (IP: %s)", acquiredTun.opts.Id, ip)
	}

	// 5. Release and clean up
	pool.ReleaseIP("1.1.1.1")
	pool.ReleaseIP("2.2.2.2")

	// Acquire again without region, should return from global pool
	ip, acquiredTun = pool.AcquireIP("")
	if acquiredTun == nil {
		t.Fatalf("expected to acquire any tunnel without region constraint")
	}
}

type mockNodeSource struct {
	pool *IPPool
}

func (m *mockNodeSource) AcquireExclusiveNode(ctx context.Context, criteria AllocationCriteria) (string, *Tunnel, error) {
	ip, tun := m.pool.AcquireIP(criteria.Region)
	if tun == nil {
		return "", nil, fmt.Errorf("no free ip found")
	}
	return ip, tun, nil
}
func (m *mockNodeSource) ReleaseExclusiveNodes(nodeIDs []string, ips []string) {}
func (m *mockNodeSource) GetLocalTunnel(nodeID string) *Tunnel                  { return nil }
func (m *mockNodeSource) SwitchNodeForUser(user *model.User) error              { return nil }
func (m *mockNodeSource) AcquirePollingNode() (string, *Tunnel, error)         { return "", nil, nil }
