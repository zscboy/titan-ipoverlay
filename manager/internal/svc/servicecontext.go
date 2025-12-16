package svc

import (
	"context"
	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/config"
	"titan-ipoverlay/manager/internal/push"

	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type Pop struct {
	API        serverapi.ServerAPI
	Socks5Addr string
	// WSServerURL string
	Area        string
	Name        string
	CountryCode string
}

type ServiceContext struct {
	Config        config.Config
	Redis         *redis.Redis
	JwtMiddleware rest.Middleware
	Pops          map[string]*Pop
	PushManager   *push.PushManager
}

func NewServiceContext(c config.Config) *ServiceContext {
	redis := redis.MustNewRedis(c.Redis)
	return &ServiceContext{
		Config:      c,
		Redis:       redis,
		Pops:        newPops(c),
		PushManager: push.NewPushManager(redis),
	}
}

// TODO: can not get server info in here, server may be stop
func newPops(c config.Config) map[string]*Pop {
	servers := make(map[string]*Pop)
	for _, pop := range c.Pops {
		api := serverapi.NewServerAPI(zrpc.MustNewClient(pop.RpcClient))
		resp, err := api.GetServerInfo(context.Background(), &serverapi.Empty{})
		if err != nil {
			panic("Get server info failed:" + err.Error())
		}
		servers[pop.Id] = &Pop{API: api, Socks5Addr: resp.Socks5Addr, Area: pop.Area, Name: pop.Name, CountryCode: pop.CountryCode}
	}
	return servers
}
