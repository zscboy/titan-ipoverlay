package ws

import (
	"fmt"
	"net"
	"titan-ipoverlay/ippop/model"
)

// Kick implements svc.NodeManager interface
func (tm *TunnelManager) Kick(nodeID string) error {
	return tm.KickNode(nodeID)
}

// SwitchNode implements svc.UserManager interface
func (tm *TunnelManager) SwitchNode(userName string) error {
	user, err := model.GetUser(tm.redis, userName)
	if err != nil {
		return err
	}

	if user == nil {
		return fmt.Errorf("handleNodeOffline, user %s not exist", userName)
	}

	return tm.SwitchNodeForUser(user)
}

// DeleteCache implements svc.UserManager interface
func (tm *TunnelManager) DeleteCache(userName string) error {
	tm.DeleteUserFromCache(userName)
	return nil
}

// GetAuth implements svc.EndpointProvider interface
func (tm *TunnelManager) GetAuth() (secret string, expire int64, err error) {
	return tm.config.JwtAuth.AccessSecret, tm.config.JwtAuth.AccessExpire, nil
}

// GetWSURL implements svc.EndpointProvider interface
func (tm *TunnelManager) GetWSURL() (string, error) {
	domain := tm.config.Socks5.ServerIP
	if len(tm.config.WS.Domain) > 0 {
		domain = tm.config.WS.Domain
	}
	return fmt.Sprintf("ws://%s:%d/ws/node", domain, tm.config.WS.Port), nil
}

// GetSocks5Addr implements svc.EndpointProvider interface
func (tm *TunnelManager) GetSocks5Addr() (string, error) {
	_, port, err := net.SplitHostPort(tm.config.Socks5.Addr)
	if err != nil {
		return "", err
	}
	socks5Addr := fmt.Sprintf("%s:%s", tm.config.Socks5.ServerIP, port)
	return socks5Addr, nil
}

// AddBlacklist implements svc.BlacklistManager interface
func (tm *TunnelManager) AddBlacklist(ips []string) error {
	if err := model.AddBlacklist(tm.redis, ips); err != nil {
		return err
	}
	for _, ip := range ips {
		tm.ipBlacklist.Store(ip, struct{}{})
		tm.ipPool.RemoveIP(ip)
	}
	return nil
}

// RemoveBlacklist implements svc.BlacklistManager interface
func (tm *TunnelManager) RemoveBlacklist(ips []string) error {
	if err := model.RemoveBlacklist(tm.redis, ips); err != nil {
		return err
	}
	for _, ip := range ips {
		tm.ipBlacklist.Delete(ip)
	}
	return nil
}
