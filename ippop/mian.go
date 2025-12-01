package main

import (
	"flag"
	"net/http"
	_ "net/http/pprof"
	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/httpproxy"
	rpc "titan-ipoverlay/ippop/rpc/export"
	"titan-ipoverlay/ippop/socks5"
	"titan-ipoverlay/ippop/ws"

	"github.com/zeromicro/go-zero/core/conf"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/service"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
)

var configFile = flag.String("f", "etc/server.yaml", "the config file")

func initPprof(listenAddr string) {
	go func() {
		logx.Info("pprof listening on ", listenAddr)
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			logx.Error(err)
		}
	}()
}

func newWS(config config.Config, tunMgr *ws.TunnelManager) *rest.Server {
	server := rest.MustNewServer(config.WS.RestConf)

	jwtMiddleware := rest.WithJwt(config.JwtAuth.AccessSecret)

	nodews := ws.NewNodeWS(tunMgr)
	nodePop := ws.NewNodePop(&config)

	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/ws/node",
		Handler: nodews.ServeWS,
	}, jwtMiddleware)
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/node/pop",
		Handler: nodePop.ServeNodePop,
	})

	return server

}

func newSocks5(c config.Config, handler socks5.Socks5Handler) *socks5.Socks5Server {
	opts := &socks5.Socks5ServerOptions{
		Address:      c.Socks5.Addr,
		UDPServerIP:  c.Socks5.ServerIP,
		UDPPortStart: c.Socks5.UDPPortStart,
		UDPPortEnd:   c.Socks5.UDPPortEnd,
		EnableAuth:   c.Socks5.EnableAuth,
		Handler:      handler,
	}
	socks5, err := socks5.New(opts)
	if err != nil {
		panic(err)
	}

	return socks5
}

func main() {
	flag.Parse()

	var c config.Config
	conf.MustLoad(*configFile, &c)
	logx.MustSetup(c.Log)

	if c.Pprof.Enable {
		initPprof(c.Pprof.ListenAddr)
	}

	var rdb = redis.MustNewRedis(c.Redis)
	var tunMgr = ws.NewTunnelManager(c, rdb)

	var ws = newWS(c, tunMgr)
	var socks5 = newSocks5(c, tunMgr)

	c.RPCServer.Redis = redis.RedisKeyConf{RedisConf: c.Redis}

	group := service.NewServiceGroup()
	group.Add(ws)
	group.Add(socks5)

	rpcServer := rpc.NewRPCServer(c.RPCServer, tunMgr, tunMgr, tunMgr)
	group.Add(rpcServer)

	if len(c.HTTPProxy) > 0 {
		httpProxyServer := httpproxy.NewServer(c.HTTPProxy, c.Redis)
		group.Add(httpProxyServer)
	}

	logx.Infof("Starting ws server at %s:%d", c.WS.Host, c.WS.Port)
	logx.Infof("Starting rpc server at %s...", c.RPCServer.ListenOn)

	group.Start()

}
