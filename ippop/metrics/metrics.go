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
		Name:    "ippop_session_duration_ms",
		Help:    "会话持续时间（毫秒）",
		Buckets: []float64{10, 50, 100, 500, 1000, 5000, 10000, 30000, 60000, 300000}, // 10ms ~ 5min
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

	// T1/T2/T3 性能指标
	// T1: Client → IPPop (WebSocket 接收) - 从远程客户端接收数据
	T1Throughput = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ippop_t1_throughput_mbps",
		Help:    "T1 吞吐量 (Client→IPPop via WebSocket) MB/s",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500},
	}, []string{"user"})

	T1Bytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_t1_bytes_total",
		Help: "T1 总字节数 (Client→IPPop via WebSocket)",
	}, []string{"user"})

	// T2: 内部处理 - WebSocket 读取完成到 SOCKS5 写入开始
	T2ProcessingTime = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ippop_t2_processing_microseconds",
		Help:    "T2 内部处理时间 (微秒)",
		Buckets: []float64{10, 50, 100, 500, 1000, 5000, 10000, 50000},
	}, []string{"user"})

	// T3: IPPop → 用户 (SOCKS5 发送)
	T3Throughput = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "ippop_t3_throughput_mbps",
		Help:    "T3 吞吐量 (IPPop→User via SOCKS5) MB/s",
		Buckets: []float64{0.1, 0.5, 1, 5, 10, 20, 50, 100, 200, 500},
	}, []string{"user"})

	T3Bytes = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_t3_bytes_total",
		Help: "T3 总字节数 (IPPop→User via SOCKS5)",
	}, []string{"user"})

	// 瓶颈检测
	BottleneckDetection = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_bottleneck_detection_total",
		Help: "瓶颈检测统计",
	}, []string{"type", "user"}) // type: t1_slow/t2_slow/t3_slow/balanced

	// 多维度流量统计 (Task 4)
	DomainTraffic = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_traffic_by_domain_bytes_total",
		Help: "按域名统计的流量总字节数",
	}, []string{"user", "domain"})

	CountryTraffic = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ippop_traffic_by_country_bytes_total",
		Help: "按国家/地区统计的流量总字节数",
	}, []string{"user", "country"})
)
