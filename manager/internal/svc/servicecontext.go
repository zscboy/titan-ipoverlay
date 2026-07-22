package svc

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"
	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/config"
	"titan-ipoverlay/manager/model"

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

type NodeCacheItem struct {
	PopID       string
	IP          string
	CountryCode string
}

type ServiceContext struct {
	Config         config.Config
	Redis          *redis.Redis
	JwtMiddleware  rest.Middleware
	Pops           map[string]*Pop
	IPGroup        *singleflight.Group
	RegionStrategy map[string][]string
	VendorStrategy map[string][]string
	BlacklistMap   sync.Map
	StrategyRR     sync.Map // map[string]*uint64
	NodePopCache   sync.Map // map[string]*NodeCacheItem
	HTTPClient     *http.Client
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

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	sc := &ServiceContext{
		Config:         c,
		Redis:          redis,
		Pops:           newPops(c),
		IPGroup:        &singleflight.Group{},
		RegionStrategy: regionStrategy,
		VendorStrategy: vendorStrategy,
		HTTPClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Second,
		},
	}

	sc.loadBlacklist()
	return sc
}

func (sc *ServiceContext) loadBlacklist() {
	now := time.Now()
	ips, err := model.GetAllIPBlacklist(sc.Redis)
	if err != nil {
		fmt.Printf("loadBlacklist error: %v\n", err)
		return
	}

	for _, ip := range ips {
		sc.BlacklistMap.Store(ip, true)
	}
	fmt.Printf("loaded %d blacklisted ips into sync.Map, cost %v\n", len(ips), time.Since(now))
}

// TODO: can not get server info in here, server may be stop
func newPops(c config.Config) map[string]*Pop {
	now := time.Now()
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
	fmt.Printf("loaded %d pop into sync.Map, cost %v\n", len(pops), time.Since(now))
	return pops
}
