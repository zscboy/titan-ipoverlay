package svc

import (
	"titan-ipoverlay/ippop/rpc/internal/config"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type NodeManager interface {
	Kick(nodeID string) error
	KickByIPs(ips []string) error
}

type UserManager interface {
	SwitchNode(userName string) error
	DeleteCache(userName string) error
}

type EndpointProvider interface {
	GetAuth() (secret string, expire int64, err error)
	GetWSURL() (string, error)
	GetSocks5Addr() (string, error)
}

type BlacklistManager interface {
	AddBlacklist(ips []string) error
	RemoveBlacklist(ips []string) error
}

type TunnelManager interface {
	NodeManager
	UserManager
	EndpointProvider
	BlacklistManager
}

type ServiceContext struct {
	Config config.Config
	Redis  *redis.Redis
	TunnelManager
}

func NewServiceContext(c config.Config) *ServiceContext {
	redis := redis.MustNewRedis(c.Redis.RedisConf)
	return &ServiceContext{
		Config: c,
		Redis:  redis,
	}
}
