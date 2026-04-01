package main

import (
	"flag"
	_ "net/http/pprof"
	"titan-ipoverlay/ippop/config"
	httpproxy "titan-ipoverlay/ippop/http"
	"titan-ipoverlay/ippop/metrics"
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

func newWS(config config.Config, tunMgr *ws.TunnelManager) *rest.Server {
	server := rest.MustNewServer(config.WS.RestConf)

	jwtMiddleware := rest.WithJwt(config.JwtAuth.AccessSecret)

	nodews := ws.NewNodeWS(tunMgr)
	nodePop := ws.NewNodePop(&config)
	sessionQuery := ws.NewSessionQuery(tunMgr)
	statsQuery := ws.NewStatsQuery(tunMgr)
	ippoolQuery := ws.NewIPPoolQuery(tunMgr)
	uploadTest := ws.NewUploadTestHandler(tunMgr)
	nodeList := ws.NewNodeListQuery(tunMgr)
	qosHandler := ws.NewQoSHandler(tunMgr)

	// ws routes
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
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/session/query",
		Handler: sessionQuery.ServeSessionQuery,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/pop/stats",
		Handler: statsQuery.ServeStats,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/ippool/free",
		Handler: ippoolQuery.ServeFreeIPs,
	})
	server.AddRoute(rest.Route{
		Method:  "POST",
		Path:    "/api/tunnel/upload-test",
		Handler: uploadTest.ServeUploadTest,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/api/tunnel/upload-test-result",
		Handler: uploadTest.ServeUploadTestResult,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/node/online/csv",
		Handler: nodeList.ServeNodeListCSV,
	})

	// QoS 运维接口
	server.AddRoute(rest.Route{
		Method:  "POST",
		Path:    "/api/qos/blacklist/add",
		Handler: qosHandler.ServeBlacklistAdd,
	})
	server.AddRoute(rest.Route{
		Method:  "POST",
		Path:    "/api/qos/blacklist/remove",
		Handler: qosHandler.ServeBlacklistRemove,
	})
	server.AddRoute(rest.Route{
		Method:  "POST",
		Path:    "/api/qos/blacklist/clear",
		Handler: qosHandler.ServeBlacklistClear,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/api/qos/blacklist/audit",
		Handler: qosHandler.ServeBlacklistAudit,
	})
	server.AddRoute(rest.Route{
		Method:  "GET",
		Path:    "/api/qos/blacklist/list",
		Handler: qosHandler.ServeBlacklistList,
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
		metrics.StartPprofServer(c.Pprof.ListenAddr)
	}

	if c.Metrics.Enable {
		metrics.StartMetricsServer(c.Metrics.ListenAddr)
	}

	var rdb = redis.MustNewRedis(c.Redis)
	var tunMgr = ws.NewTunnelManager(c, rdb)

	var ws = newWS(c, tunMgr)
	var socks5 = newSocks5(c, tunMgr)

	c.RPCServer.Redis = redis.RedisKeyConf{RedisConf: c.Redis}

	group := service.NewServiceGroup()
	group.Add(ws)
	group.Add(socks5)

	rpcServer := rpc.NewRPCServer(c.RPCServer, tunMgr)
	group.Add(rpcServer)

	if len(c.HTTPProxy) > 0 {
		// httpProxyServer := httpproxy.NewServer(c.HTTPProxy, c.Redis)
		httpProxyServer := httpproxy.NewServer(c.HTTPProxy, tunMgr)
		group.Add(httpProxyServer)
	}

	logx.Infof("Starting ws server at %s:%d", c.WS.Host, c.WS.Port)
	logx.Infof("Starting rpc server at %s...", c.RPCServer.ListenOn)

	group.Start()

}
