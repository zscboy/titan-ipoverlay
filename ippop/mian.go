package main

import (
	"flag"
	"fmt"
	api "titan-ipoverlay/ippop/api/export"
	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/httpproxy"
	rpc "titan-ipoverlay/ippop/rpc/export"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

var configFile = flag.String("f", "etc/server.yaml", "the config file")

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)

	logx.MustSetup(c.Log)
	// Override Redis and APIServer
	c.RPCServer.Redis = redis.RedisKeyConf{RedisConf: c.Redis}
	c.RPCServer.APIServer = fmt.Sprintf("localhost:%d", c.APIServer.Port)

	group := service.NewServiceGroup()
	api.AddAPIService(group, c.APIServer)

	rpcServer := rpc.NewRPCServer(c.RPCServer)
	group.Add(rpcServer)

	if len(c.HTTPProxy) > 0 {
		httpProxyServer := httpproxy.NewServer(c.HTTPProxy, c.Redis)
		group.Add(httpProxyServer)
	}

	logx.Infof("Starting api server at %s:%d", c.APIServer.Host, c.APIServer.Port)
	logx.Infof("Starting rpc server at %s...", c.RPCServer.ListenOn)

	group.Start()

}
