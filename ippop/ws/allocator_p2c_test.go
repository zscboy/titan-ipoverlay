package ws

import (
	"context"
	"testing"
	"titan-ipoverlay/ippop/model"
)

// mockSource 记录哪个方法被调用。
type mockSource struct {
	p2cCalled, plainCalled bool
}

func (m *mockSource) AcquireExclusiveNode(ctx context.Context) (string, *Tunnel, error) {
	return "", nil, nil
}
func (m *mockSource) ReleaseExclusiveNodes(nodeIDs []string, ips []string) {}
func (m *mockSource) GetLocalTunnel(nodeID string) *Tunnel                 { return nil }
func (m *mockSource) SwitchNodeForUser(user *model.User) error             { return nil }
func (m *mockSource) AcquirePollingNode() (string, *Tunnel, error) {
	m.plainCalled = true
	return "1.1.1.1", &Tunnel{opts: &TunOptions{}}, nil
}
func (m *mockSource) AcquireP2CPollingNode() (string, *Tunnel, error) {
	m.p2cCalled = true
	return "2.2.2.2", &Tunnel{opts: &TunOptions{}}, nil
}

func TestPollingAllocator_RoutesByUserFlag(t *testing.T) {
	src := &mockSource{}
	a := NewPollingAllocator(src)

	if _, _, err := a.Allocate(&model.User{P2CPolling: 1}, nil); err != nil {
		t.Fatal(err)
	}
	if !src.p2cCalled || src.plainCalled {
		t.Fatalf("P2CPolling=1 must route to AcquireP2CPollingNode (p2c=%v plain=%v)", src.p2cCalled, src.plainCalled)
	}

	src2 := &mockSource{}
	a2 := NewPollingAllocator(src2)
	if _, _, err := a2.Allocate(&model.User{}, nil); err != nil {
		t.Fatal(err)
	}
	if src2.p2cCalled || !src2.plainCalled {
		t.Fatalf("default user must route to AcquirePollingNode (p2c=%v plain=%v)", src2.p2cCalled, src2.plainCalled)
	}

	// nil user 防御：走原路径
	src3 := &mockSource{}
	a3 := NewPollingAllocator(src3)
	if _, _, err := a3.Allocate(nil, nil); err != nil {
		t.Fatal(err)
	}
	if src3.p2cCalled || !src3.plainCalled {
		t.Fatal("nil user must route to plain polling")
	}
}
