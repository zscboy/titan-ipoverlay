package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// 隧道指标
	ActiveTunnels = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ippop_active_tunnels",
		Help: "当前活跃的 WebSocket 隧道数量",
	})

	TunnelDelay = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ippop_tunnel_delay_ms",
		Help: "隧道延迟（毫秒）",
	}, []string{"tunnel_id"})

	// 会话指标
	ActiveSessions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ippop_active_sessions",
		Help: "当前活跃的代理会话数量",
	})

	TotalSessions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ippop_total_sessions",
		Help: "总会话数（累计）",
	})

	SessionDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "ippop_session_duration_seconds",
		Help:    "会话持续时间（秒）",
		Buckets: prometheus.DefBuckets, // [0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10]
	})

	// 连接指标
	SOCKS5Connections = promauto.NewCounter(prometheus.CounterOpts{
		Name: "ippop_socks5_connections_total",
		Help: "SOCKS5 连接总数",
	})

	SOCKS5Errors = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_socks5_errors_total",
		Help: "SOCKS5 错误总数",
	}, []string{"error_type"})

	// 流量指标
	BytesReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_bytes_received_total",
		Help: "接收字节数（从 SOCKS5 客户端）",
	}, []string{"user"})

	BytesSent = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_bytes_sent_total",
		Help: "发送字节数（到 SOCKS5 客户端）",
	}, []string{"user"})

	// 用户指标
	ActiveUsers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ippop_active_users",
		Help: "当前活跃用户数",
	})

	UserAuthFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_user_auth_failures_total",
		Help: "用户认证失败次数",
	}, []string{"username"})

	UserTraffic = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ippop_user_traffic_bytes",
		Help: "用户当前流量使用（字节）",
	}, []string{"username"})

	// 性能指标
	RequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ippop_request_duration_seconds",
		Help:    "请求处理时间",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "status"})

	// WebSocket 指标
	WebSocketMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_websocket_messages_total",
		Help: "WebSocket 消息总数",
	}, []string{"type", "direction"}) // direction: sent/received

	// 半关闭指标（新增）
	HalfCloseEvents = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_half_close_events_total",
		Help: "TCP 半关闭事件总数",
	}, []string{"direction"}) // direction: client_to_server/server_to_client
)
