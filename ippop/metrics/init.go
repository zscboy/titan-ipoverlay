package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
)

// Init 注册所有指标到默认的 Prometheus Registry
func Init() {
	prometheus.MustRegister(ActiveTunnels)
	prometheus.MustRegister(TunnelDelay)
	prometheus.MustRegister(ActiveSessions)
	prometheus.MustRegister(TotalSessions)
	prometheus.MustRegister(SessionDuration)
	prometheus.MustRegister(SOCKS5Connections)
	prometheus.MustRegister(SOCKS5Errors)
	prometheus.MustRegister(BytesReceived)
	prometheus.MustRegister(BytesSent)
	prometheus.MustRegister(ActiveUsers)
	prometheus.MustRegister(UserAuthFailures)
	prometheus.MustRegister(UserTraffic)
	prometheus.MustRegister(RequestDuration)
	prometheus.MustRegister(WebSocketMessages)
	prometheus.MustRegister(HalfCloseEvents)

}

// StartMetricsServer 启动 Prometheus HTTP 服务器
func StartMetricsServer(listenAddr string) {
	Init()
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		logx.Infof("Prometheus metrics listening on %s", listenAddr)
		if err := http.ListenAndServe(listenAddr, mux); err != nil {
			logx.Errorf("Prometheus metrics server error: %v", err)
		}
	}()
}

// StartPprofServer 启动 pprof HTTP 服务器
func StartPprofServer(listenAddr string) {
	go func() {
		logx.Infof("pprof listening on http://%s", listenAddr)
		// 使用默认的 http.DefaultServeMux，pprof 会自动注册到上面
		if err := http.ListenAndServe(listenAddr, nil); err != nil {
			logx.Errorf("pprof server error: %v", err)
		}
	}()
}
