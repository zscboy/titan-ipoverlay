package ws

import (
	"container/list"
	"context"
	"sync"
	"time"
	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/socks5"
)

// NodeSource defines the capabilities required from the tunnel provider.
type NodeSource interface {
	AcquireExclusiveNode(ctx context.Context) (*Tunnel, error)
	ReleaseExclusiveNodes(nodeIDs []string)
	GetLocalTunnel(nodeID string) *Tunnel
	PickActiveTunnel() (*Tunnel, error)
}

// UserSession tracks the binding between a user session and a node.
type UserSession struct {
	username     string
	sessionID    string
	deviceID     string
	connectCount int32         // Reference count of active connections
	disconnectAt time.Time     // Time when connectCount became zero
	idleElement  *list.Element // Pointer to the element in SessionManager's idleList
}

// NodeAllocator is the interface for different node assignment strategies.
type NodeAllocator interface {
	Allocate(user *model.User, target *socks5.SocksTargetInfo) (*Tunnel, *UserSession, error)
}

// AllocatorRegistry manages different allocation strategies based on RouteMode.
type AllocatorRegistry struct {
	lock       sync.RWMutex
	allocators map[model.RouteMode]NodeAllocator
}

func NewAllocatorRegistry() *AllocatorRegistry {
	return &AllocatorRegistry{
		allocators: make(map[model.RouteMode]NodeAllocator),
	}
}

func (r *AllocatorRegistry) Register(mode model.RouteMode, alloc NodeAllocator) {
	r.lock.Lock()
	defer r.lock.Unlock()
	r.allocators[mode] = alloc
}

func (r *AllocatorRegistry) Get(mode model.RouteMode) NodeAllocator {
	r.lock.RLock()
	defer r.lock.RUnlock()
	return r.allocators[mode]
}

// StaticAllocator handles modes where the node is pre-assigned to the user.
type StaticAllocator struct {
	source NodeSource
}

func NewStaticAllocator(source NodeSource) *StaticAllocator {
	return &StaticAllocator{source: source}
}

// TODO: if tunnel offline, need to allocate a new one
func (a *StaticAllocator) Allocate(user *model.User, target *socks5.SocksTargetInfo) (*Tunnel, *UserSession, error) {
	tun := a.source.GetLocalTunnel(user.RouteNodeID)
	return tun, nil, nil
}
