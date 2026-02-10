package ws

import (
	"context"
	"fmt"
	"time"

	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/metrics"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// 批量刷新配置
	maxBufferSize = 1000             // 缓冲区满时触发写入
	flushInterval = 10 * time.Second // 定时刷新间隔
)

// ClickHouse 建表 SQL (按小时分区，3天TTL)
const createTableSQL = `
CREATE TABLE IF NOT EXISTS session_perf (
    session_id    String,
    user_name     String,
    target_domain String,
    country_code  String,
    node_id       String,
    duration_sec  Float64,
    t1_bytes_mb   Float64,
    t1_speed_mbps Float64,
    t1_count      Int64,
    t2_avg_us     Int64,
    t2_total_ms   Int64,
    t2_count      Int64,
    t3_bytes_mb   Float64,
    t3_speed_mbps Float64,
    t3_count      Int64,
    bottleneck    String,
    created_at    DateTime DEFAULT now()
) ENGINE = MergeTree()
PARTITION BY toStartOfHour(created_at)
ORDER BY (user_name, created_at)
TTL created_at + INTERVAL 3 DAY
`

// SessionPerfRecord 会话性能记录
type SessionPerfRecord struct {
	SessionID    string  `json:"sid"`
	UserName     string  `json:"user"`
	TargetDomain string  `json:"domain"`
	CountryCode  string  `json:"country"`
	DurationSec  float64 `json:"dur"`
	T1BytesMB    float64 `json:"t1_mb"`
	T1SpeedMBps  float64 `json:"t1_spd"`
	T1Count      int64   `json:"t1_cnt"`
	T2AvgUs      int64   `json:"t2_us"`
	T2TotalMs    int64   `json:"t2_ms"`
	T2Count      int64   `json:"t2_cnt"`
	T3BytesMB    float64 `json:"t3_mb"`
	T3SpeedMBps  float64 `json:"t3_spd"`
	T3Count      int64   `json:"t3_cnt"`
	UploadBytes  int64   `json:"up_bytes"`
	Bottleneck   string  `json:"btn"`
	Timestamp    int64   `json:"ts"`
}

const (
	evtStart = iota
	evtEnd
	evtSocksError
	evtAuthFailure
	evtTrafficDelta
)

// trafficDelta 增量统计
type trafficDelta struct {
	t1Bytes int64
	t1Dur   time.Duration
	t2Dur   time.Duration
	t3Bytes int64
	t3Dur   time.Duration
	upBytes int64
}

// collectorEvent 内部事件
type collectorEvent struct {
	typ      int // 事件类型
	userName string
	record   SessionPerfRecord
	tag      string       // 用于存放 error_type 等标签
	delta    trafficDelta // 增量统计
}

// userMetrics 缓存用户常用的 Prometheus Counter，避免高频 label 查找
type userMetrics struct {
	t1Bytes prometheus.Counter
	t3Bytes prometheus.Counter
	rxBytes prometheus.Counter
	txBytes prometheus.Counter
}

// SessionPerfCollector 会话性能数据批量收集器
type SessionPerfCollector struct {
	clickhouse driver.Conn
	chEnabled  bool
	nodeID     string
	// 通道配置
	chBufferChan chan *SessionPerfRecord // 无锁 channel buffer
	metricsChan  chan *collectorEvent
	// 采集协程内部状态
	userSessions map[string]int          // 维护用户 -> 会话数
	metricsCache map[string]*userMetrics // 缓存用户 Counter
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewSessionPerfCollector 创建新的收集器
func NewSessionPerfCollector(chConfig config.ClickHouse, nodeID string) *SessionPerfCollector {
	ctx, cancel := context.WithCancel(context.Background())

	c := &SessionPerfCollector{
		chEnabled:    chConfig.Enable,
		nodeID:       nodeID,
		chBufferChan: make(chan *SessionPerfRecord, maxBufferSize*2), // ClickHouse 记录缓冲，应对会话爆发
		metricsChan:  make(chan *collectorEvent, 5000000),            // 500万缓冲区，应对 8Gbps 爆发
		userSessions: make(map[string]int),
		metricsCache: make(map[string]*userMetrics),
		ctx:          ctx,
		cancel:       cancel,
	}

	// 初始化 ClickHouse 连接
	if chConfig.Enable {
		conn, err := clickhouse.Open(&clickhouse.Options{
			Addr: []string{chConfig.Addr},
			Auth: clickhouse.Auth{
				Database: chConfig.Database,
				Username: chConfig.Username,
				Password: chConfig.Password,
			},
			Settings: clickhouse.Settings{
				"max_execution_time": 60,
			},
			DialTimeout:     10 * time.Second,
			MaxOpenConns:    5,
			MaxIdleConns:    2,
			ConnMaxLifetime: time.Hour,
		})
		if err != nil {
			logx.Errorf("SessionPerfCollector: ClickHouse connect error: %v", err)
		} else {
			c.clickhouse = conn
			// 自动建表 (增加 node_id 字段的支持)
			if err := conn.Exec(ctx, createTableSQL); err != nil {
				logx.Errorf("SessionPerfCollector: Create table error: %v", err)
			} else {
				logx.Info("SessionPerfCollector: ClickHouse connected and table ready")
			}
		}
	}

	return c
}

// Start 启动后台刷新协程
func (c *SessionPerfCollector) Start() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	// 初始值设为 0，确保指标被 Prometheus 发现
	metrics.ActiveUsers.WithLabelValues(c.nodeID).Set(0)
	metrics.ActiveSessions.WithLabelValues(c.nodeID).Set(0)

	logx.Info("SessionPerfCollector started")

	// 启动 ClickHouse 批量写入协程，避免阻塞指标采集流程
	go c.clickhouseWorker()

	for {
		select {
		case event := <-c.metricsChan:
			switch event.typ {
			case evtStart:
				c.handleSessionStart(event.userName)
			case evtEnd:
				c.handleSessionEnd(&event.record)
			case evtSocksError:
				metrics.SOCKS5Errors.WithLabelValues(event.tag, c.nodeID).Inc()
			case evtAuthFailure:
				metrics.UserAuthFailures.WithLabelValues(event.userName, c.nodeID).Inc()
			case evtTrafficDelta:
				c.handleTrafficDelta(event.userName, event.delta)
			}
		case <-c.ctx.Done():
			if c.clickhouse != nil {
				c.clickhouse.Close()
			}
			logx.Info("SessionPerfCollector stopped")
			return
		}
	}
}

// clickhouseWorker 独立的 ClickHouse 批量写入协程
func (c *SessionPerfCollector) clickhouseWorker() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.Flush()
		case <-c.ctx.Done():
			c.Flush() // 退出前最后刷新一次
			return
		}
	}
}

// handleSessionStart 处理会话开始指标
func (c *SessionPerfCollector) handleSessionStart(userName string) {
	metrics.SOCKS5Connections.WithLabelValues(userName, c.nodeID).Inc()
	metrics.TotalSessions.WithLabelValues(userName, c.nodeID).Inc()
	metrics.ActiveSessions.WithLabelValues(userName, c.nodeID).Inc()

	// 更新活跃用户数
	if count := c.userSessions[userName]; count == 0 {
		metrics.ActiveUsers.WithLabelValues(userName, c.nodeID).Inc()
	}
	c.userSessions[userName]++
}

// handleSessionEnd 处理会话结束指标
func (c *SessionPerfCollector) handleSessionEnd(r *SessionPerfRecord) {
	// 基础指标
	metrics.ActiveSessions.WithLabelValues(r.UserName, c.nodeID).Dec()
	metrics.SessionDuration.WithLabelValues(r.UserName, c.nodeID).Observe(r.DurationSec)

	// 更新活跃用户数
	if count := c.userSessions[r.UserName]; count > 0 {
		c.userSessions[r.UserName]--
		if c.userSessions[r.UserName] == 0 {
			metrics.ActiveUsers.WithLabelValues(r.UserName, c.nodeID).Dec()
			delete(c.userSessions, r.UserName)
		}
	}

	// 注意：T1Bytes, T3Bytes, BytesSent, BytesReceived 等累加型指标已通过 evtTrafficDelta 增量上报
	// 这里主要处理直方图和需要全会话数据的汇总指标
	if r.T1Count > 0 {
		metrics.T1Throughput.WithLabelValues(r.UserName, c.nodeID).Observe(r.T1SpeedMBps)
	}

	// T2 指标 (平均处理时间)
	if r.T2Count > 0 {
		metrics.T2ProcessingTime.WithLabelValues(r.UserName, c.nodeID).Observe(float64(r.T2AvgUs))
	}

	// T3 指标
	if r.T3Count > 0 {
		metrics.T3Throughput.WithLabelValues(r.UserName, c.nodeID).Observe(r.T3SpeedMBps)
	}

	// 瓶颈检测
	metrics.BottleneckDetection.WithLabelValues(r.Bottleneck, r.UserName, c.nodeID).Inc()

	// 多维度流量统计 (Task 4)
	totalBytes := (r.T1BytesMB + r.T3BytesMB) * 1024 * 1024
	if r.TargetDomain != "" {
		metrics.DomainTraffic.WithLabelValues(r.UserName, r.TargetDomain, c.nodeID).Add(totalBytes)
	}
	if r.CountryCode != "" {
		metrics.CountryTraffic.WithLabelValues(r.UserName, r.CountryCode, c.nodeID).Add(totalBytes)
	}
}

// Stop 停止收集器
func (c *SessionPerfCollector) Stop() {
	c.cancel()
}

// ReportSessionStart 异步上报会话开始
func (c *SessionPerfCollector) ReportSessionStart(userName string) {
	select {
	case c.metricsChan <- &collectorEvent{typ: evtStart, userName: userName}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping start event")
	}
}

// ReportTrafficDelta 异步增量上报统计（非阻塞）
func (c *SessionPerfCollector) ReportTrafficDelta(userName string, delta trafficDelta) {
	select {
	case c.metricsChan <- &collectorEvent{typ: evtTrafficDelta, userName: userName, delta: delta}:
	default:
		// 避免阻塞热路径，如果队列满了则忽略
	}
}

func (c *SessionPerfCollector) getOrUpdateUserMetrics(userName string) *userMetrics {
	if m, ok := c.metricsCache[userName]; ok {
		return m
	}

	m := &userMetrics{
		t1Bytes: metrics.T1Bytes.WithLabelValues(userName, c.nodeID),
		t3Bytes: metrics.T3Bytes.WithLabelValues(userName, c.nodeID),
		rxBytes: metrics.BytesReceived.WithLabelValues(userName, c.nodeID),
		txBytes: metrics.BytesSent.WithLabelValues(userName, c.nodeID),
	}
	c.metricsCache[userName] = m
	return m
}

func (c *SessionPerfCollector) handleTrafficDelta(userName string, d trafficDelta) {
	m := c.getOrUpdateUserMetrics(userName)

	if d.t1Bytes > 0 {
		m.t1Bytes.Add(float64(d.t1Bytes))
	}
	if d.t3Bytes > 0 {
		m.t3Bytes.Add(float64(d.t3Bytes))
		m.txBytes.Add(float64(d.t3Bytes))
	}
	if d.upBytes > 0 {
		m.rxBytes.Add(float64(d.upBytes))
	}
}

// ReportSOCKS5Error 异步上报 SOCKS5 错误
func (c *SessionPerfCollector) ReportSOCKS5Error(errType string) {
	select {
	case c.metricsChan <- &collectorEvent{typ: evtSocksError, tag: errType}:
	default:
	}
}

// ReportAuthFailure 异步上报认证失败
func (c *SessionPerfCollector) ReportAuthFailure(userName string) {
	select {
	case c.metricsChan <- &collectorEvent{typ: evtAuthFailure, userName: userName}:
	default:
	}
}

// Collect 添加数据到缓冲区（完全无锁）
func (c *SessionPerfCollector) Collect(record *SessionPerfRecord) {
	// 1. 异步发送到处理通道（用于 Prometheus 指标）
	select {
	case c.metricsChan <- &collectorEvent{typ: evtEnd, record: *record}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping end record")
	}

	// 2. 批量写入 ClickHouse（无锁发送到 channel）
	if !c.chEnabled {
		return
	}

	select {
	case c.chBufferChan <- record:
		// 检查是否需要触发异步刷新
		if len(c.chBufferChan) >= maxBufferSize {
			go c.Flush()
		}
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: ClickHouse buffer channel full, dropping record")
	}
}

// Flush 批量写入 ClickHouse（从 channel 批量读取）
func (c *SessionPerfCollector) Flush() {
	if !c.chEnabled || c.clickhouse == nil {
		return
	}

	// 从 channel 批量读取，无锁操作
	toFlush := make([]*SessionPerfRecord, 0, maxBufferSize)
	for {
		select {
		case record := <-c.chBufferChan:
			toFlush = append(toFlush, record)
			if len(toFlush) >= maxBufferSize {
				goto flush
			}
		default:
			goto flush
		}
	}
flush:
	if len(toFlush) == 0 {
		return
	}

	// 批量 INSERT
	ctx := context.Background()
	batch, err := c.clickhouse.PrepareBatch(ctx, "INSERT INTO session_perf")
	if err != nil {
		logx.Errorf("SessionPerfCollector Flush: PrepareBatch error: %v", err)
		return
	}

	for _, r := range toFlush {
		createdAt := time.Unix(r.Timestamp, 0)
		err := batch.Append(
			r.SessionID,
			r.UserName,
			r.TargetDomain,
			r.CountryCode,
			c.nodeID, // 增加 node_id
			r.DurationSec,
			r.T1BytesMB,
			r.T1SpeedMBps,
			r.T1Count,
			r.T2AvgUs,
			r.T2TotalMs,
			r.T2Count,
			r.T3BytesMB,
			r.T3SpeedMBps,
			r.T3Count,
			r.Bottleneck,
			createdAt,
		)
		if err != nil {
			logx.Errorf("SessionPerfCollector Flush: Append error: %v", err)
		}
	}

	if err := batch.Send(); err != nil {
		logx.Errorf("SessionPerfCollector Flush: Send error: %v", err)
	} else {
		logx.Debugf("SessionPerfCollector Flush: wrote %d records to ClickHouse", len(toFlush))
	}
}

// BufferSize 返回当前缓冲区大小（用于监控）
func (c *SessionPerfCollector) BufferSize() int {
	return len(c.chBufferChan)
}

// Query 查询会话数据 (示例方法)
func (c *SessionPerfCollector) QueryByUser(userName string, limit int) ([]SessionPerfRecord, error) {
	if !c.chEnabled || c.clickhouse == nil {
		return nil, fmt.Errorf("ClickHouse not enabled")
	}

	ctx := context.Background()
	rows, err := c.clickhouse.Query(ctx, `
		SELECT session_id, user_name, target_domain, duration_sec, 
		       t1_bytes_mb, t1_speed_mbps, t1_count,
		       t2_avg_us, t2_total_ms, t2_count,
		       t3_bytes_mb, t3_speed_mbps, t3_count,
		       bottleneck, toUnixTimestamp(created_at) as ts
		FROM session_perf
		WHERE user_name = ?
		ORDER BY created_at DESC
		LIMIT ?
	`, userName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SessionPerfRecord
	for rows.Next() {
		var r SessionPerfRecord
		if err := rows.Scan(
			&r.SessionID, &r.UserName, &r.TargetDomain, &r.DurationSec,
			&r.T1BytesMB, &r.T1SpeedMBps, &r.T1Count,
			&r.T2AvgUs, &r.T2TotalMs, &r.T2Count,
			&r.T3BytesMB, &r.T3SpeedMBps, &r.T3Count,
			&r.Bottleneck, &r.Timestamp,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}
