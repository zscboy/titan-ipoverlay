package svc

import (
	"context"
	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/config"

	"github.com/golang/groupcache/singleflight"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Pop struct {
	Config       config.Pop
	API          serverapi.ServerAPI
	Socks5Addr   string
	AccessSecret string
	AccessExpire int64
}

type ServiceContext struct {
	Config         config.Config
	Redis          *redis.Redis
	JwtMiddleware  rest.Middleware
	Pops           map[string]*Pop
	IPGroup        *singleflight.Group
	RegionStrategy map[string][]string
	VendorStrategy map[string][]string
}

func NewServiceContext(c config.Config) *ServiceContext {
	redis := redis.MustNewRedis(c.Redis)

	regionStrategy := make(map[string][]string)
	for _, rule := range c.Strategy.RegionRules {
		regionStrategy[rule.Key] = rule.PopIds
	}

	vendorStrategy := make(map[string][]string)
	for _, rule := range c.Strategy.VendorRules {
		vendorStrategy[rule.Key] = rule.PopIds
	}

	return &ServiceContext{
		Config:         c,
		Redis:          redis,
		Pops:           newPops(c),
		IPGroup:        &singleflight.Group{},
		RegionStrategy: regionStrategy,
		VendorStrategy: vendorStrategy,
	}
}

// TODO: can not get server info in here, server may be stop
func newPops(c config.Config) map[string]*Pop {
	pops := make(map[string]*Pop)
	for _, popCfg := range c.Pops {
		api := serverapi.NewServerAPI(zrpc.MustNewClient(popCfg.RpcClient))
		resp, err := api.GetServerInfo(context.Background(), &serverapi.Empty{})
		if err != nil {
			panic("Get server info failed:" + err.Error())
		}
		pops[popCfg.Id] = &Pop{
			Config:       popCfg,
			API:          api,
			Socks5Addr:   resp.Socks5Addr,
			AccessSecret: resp.AccessSecret,
			AccessExpire: resp.AccessExpire,
		}
	}
	return pops
}
