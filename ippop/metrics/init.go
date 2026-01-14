package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
)

// StartMetricsServer 启动 Prometheus HTTP 服务器
// 注意：不需要手动注册指标，promauto.NewXXX() 已经自动注册了
func StartMetricsServer(listenAddr string) {
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
