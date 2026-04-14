package ws

import (
	"titan-ipoverlay/ippop/model"
	"titan-ipoverlay/ippop/socks5"
)

// PollingAllocator implements NodeAllocator for Polling mode.
type PollingAllocator struct {
	source NodeSource
}

func NewPollingAllocator(source NodeSource) *PollingAllocator {
	return &PollingAllocator{source: source}
}

func (a *PollingAllocator) Allocate(user *model.User, target *socks5.SocksTargetInfo) (*Tunnel, *UserSession, error) {
	_, tun, err := a.source.AcquirePollingNode()
	if err != nil {
		return nil, nil, err
	}

	return tun, nil, nil
}
