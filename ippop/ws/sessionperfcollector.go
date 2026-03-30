package ws

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/metrics"

	_ "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// 批量刷新配置
	maxBufferSize = 1000             // 缓冲区满时触发写入
	flushInterval = 10 * time.Second // 定时刷新间隔
)

// ClickHouse 建表 SQL (按小时分区，7天TTL)
const createTableSQL = `
CREATE TABLE IF NOT EXISTS session_perf (
    session_id     String,
    user_name      String,
    target_domain  String,
    country_code   String,
    pop_node_id    String,
    client_node_id String,
    duration_sec   Float64,
    t1_bytes_mb    Float64,
    t1_speed_mbps  Float64,
    t1_count       Int64,
    t2_avg_us      Int64,
    t2_total_ms    Int64,
    t2_count       Int64,
    t3_bytes_mb    Float64,
    t3_speed_mbps  Float64,
    t3_count       Int64,
    bottleneck     String,
    created_at     DateTime DEFAULT now()
) ENGINE = MergeTree()
PARTITION BY toStartOfHour(created_at)
ORDER BY (user_name, created_at)
TTL created_at + INTERVAL 7 DAY
`

// SessionPerfRecord 会话性能记录
type SessionPerfRecord struct {
	SessionID    string  `json:"sid"`
	UserName     string  `json:"user"`
	TargetDomain string  `json:"domain"`
	CountryCode  string  `json:"country"`
	PopNodeID    string  `json:"pop_id"`
	ClientNodeID string  `json:"client_id"`
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
	evtEndManual
	evtSocksError
	evtAuthFailure
)

// UserTrafficAggregator 用户级别的原子流量聚合器
type UserTrafficAggregator struct {
	T1Bytes atomic.Int64
	T1Dur   atomic.Int64
	T2Dur   atomic.Int64
	T3Bytes atomic.Int64
	T3Dur   atomic.Int64
	UpBytes atomic.Int64

	// 上次上报给 Prometheus 的快照（用于计算增量）
	lastT1Bytes int64
	lastT1Dur   int64
	lastT2Dur   int64
	lastT3Bytes int64
	lastT3Dur   int64
	lastUpBytes int64
}

// collectorEvent 内部事件
type collectorEvent struct {
	typ      int // 事件类型
	userName string
	record   SessionPerfRecord
	tag      string // 用于存放 error_type 等标签
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
	db        *sql.DB
	chEnabled bool
	nodeID    string
	// 通道配置
	chBufferChan chan *SessionPerfRecord // 无锁 channel buffer
	metricsChan  chan *collectorEvent
	// 采集协程内部状态
	userSessions    map[string]int          // 维护用户 -> 会话数
	metricsCache    map[string]*userMetrics // 缓存用户 Counter
	userAggregators sync.Map                // 维护用户 -> *UserTrafficAggregator
	ctx             context.Context
	cancel          context.CancelFunc
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
		logx.Infof("SessionPerfCollector: Initializing ClickHouse connection to %s (Version: 2026-03-04-C)", chConfig.Addr)
		// 经过调试发现，直接使用 clickhouse.Open 可能导致协议识别错误 (packet [72])
		// 使用标准库 database/sql.Open + DSN 是最稳健的 HTTP 连接方式
		dsn := fmt.Sprintf("clickhouse://%s:%s@%s/%s?dial_timeout=10s&max_execution_time=60",
			chConfig.Username, chConfig.Password, chConfig.Addr, chConfig.Database)

		db, err := sql.Open("clickhouse", dsn)
		if err != nil {
			logx.Errorf("SessionPerfCollector: ClickHouse open error: %v", err)
		} else {
			db.SetMaxOpenConns(5)
			db.SetMaxIdleConns(2)
			db.SetConnMaxLifetime(time.Hour)

			// 验证连接并自动建表
			if err := db.Ping(); err != nil {
				logx.Errorf("SessionPerfCollector: ClickHouse ping error: %v", err)
			} else {
				c.db = db
				if _, err := db.Exec(createTableSQL); err != nil {
					logx.Errorf("SessionPerfCollector: Create table error: %v", err)
				} else {
					logx.Info("SessionPerfCollector: ClickHouse connected and table ready")
				}
			}
		}
	}

	return c
}

// sanitizeUser 清洗用户名标签，防止 Session ID 或非法输入导致基数爆炸
func (c *SessionPerfCollector) sanitizeUser(userName string) string {
	if userName == "" {
		return "_EMPTY_"
	}

	// 1. 如果包含 -session-，只截取前缀做为基础用户名
	parts := strings.Split(userName, "-session-")
	baseName := parts[0]

	// 2. 限制长度，防止恶意超长字符串
	if len(baseName) > 64 {
		baseName = baseName[:64]
	}

	return baseName
}

// Start 启动后台刷新协程
func (c *SessionPerfCollector) Start() {
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	// 5 秒一次的 Prometheus 指标同步定时器
	promTicker := time.NewTicker(5 * time.Second)
	defer promTicker.Stop()

	logx.Info("SessionPerfCollector started")

	// 启动 ClickHouse 批量写入协程，避免阻塞指标采集流程
	go c.clickhouseWorker()

	for {
		select {
		case event := <-c.metricsChan:
			switch event.typ {
			case evtStart:
				c.handleSessionStart(c.sanitizeUser(event.userName))
			case evtEnd:
				// record 中的 UserName 在创建时已经通过 sanitizeUser 预处理
				c.handlePerformanceRecord(&event.record)
			case evtEndManual:
				c.handleSessionEndManual(c.sanitizeUser(event.userName))
			case evtSocksError:
				metrics.SOCKS5Errors.WithLabelValues(event.tag, c.nodeID).Inc()
			case evtAuthFailure:
				// 对认证失败的用户名进行更严格的过滤，仅保留可能的业务用户
				sanitized := c.sanitizeUser(event.userName)
				if len(sanitized) < 3 || len(sanitized) > 32 {
					sanitized = "_INVALID_ATTEMPT_"
				}
				metrics.UserAuthFailures.WithLabelValues(sanitized, c.nodeID).Inc()
			}
		case <-promTicker.C:
			c.flushMetricsToPrometheus()
		case <-c.ctx.Done():
			if c.db != nil {
				c.db.Close()
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

// handleSessionEndManual 处理手动的会话结束（用于精确同步计数）
func (c *SessionPerfCollector) handleSessionEndManual(userName string) {
	metrics.ActiveSessions.WithLabelValues(userName, c.nodeID).Dec()

	// 更新活跃用户数
	if count := c.userSessions[userName]; count > 0 {
		c.userSessions[userName]--
		if c.userSessions[userName] == 0 {
			metrics.ActiveUsers.WithLabelValues(userName, c.nodeID).Dec()
			delete(c.userSessions, userName)
		}
	}
}

// handlePerformanceRecord 处理会话结束时的性能指标（只处理数据，Dec 逻辑由 handleSessionEndManual 负责）
func (c *SessionPerfCollector) handlePerformanceRecord(r *SessionPerfRecord) {
	sanitizedUser := c.sanitizeUser(r.UserName)
	metrics.SessionDuration.WithLabelValues(sanitizedUser, c.nodeID).Observe(r.DurationSec)

	// 这里主要处理直方图和需要全会话数据的汇总指标
	if r.T1Count > 0 {
		metrics.T1Throughput.WithLabelValues(sanitizedUser, c.nodeID).Observe(r.T1SpeedMBps)
	}

	// T2 指标 (平均处理时间)
	if r.T2Count > 0 {
		metrics.T2ProcessingTime.WithLabelValues(r.UserName, c.nodeID).Observe(float64(r.T2AvgUs))
	}

	if r.T3Count > 0 {
		metrics.T3Throughput.WithLabelValues(sanitizedUser, c.nodeID).Observe(r.T3SpeedMBps)
	}

	// 瓶颈检测
	metrics.BottleneckDetection.WithLabelValues(r.Bottleneck, sanitizedUser, c.nodeID).Inc()

	// 多维度流量统计 (Task 4)
	totalBytes := (r.T1BytesMB + r.T3BytesMB) * 1024 * 1024
	if r.TargetDomain != "" {
		metrics.DomainTraffic.WithLabelValues(sanitizedUser, r.TargetDomain, c.nodeID).Add(totalBytes)
	}
	if r.CountryCode != "" {
		metrics.CountryTraffic.WithLabelValues(sanitizedUser, r.CountryCode, c.nodeID).Add(totalBytes)
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

// ReportSessionEnd 异步上报会话结束
func (c *SessionPerfCollector) ReportSessionEnd(userName string) {
	select {
	case c.metricsChan <- &collectorEvent{typ: evtEndManual, userName: userName}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping end event")
	}
}

// GetAggregator 获取或创建用户的流量聚合器
func (c *SessionPerfCollector) GetAggregator(userName string) *UserTrafficAggregator {
	if v, ok := c.userAggregators.Load(userName); ok {
		return v.(*UserTrafficAggregator)
	}

	agg := &UserTrafficAggregator{}
	v, loaded := c.userAggregators.LoadOrStore(userName, agg)
	if loaded {
		return v.(*UserTrafficAggregator)
	}
	return agg
}

// flushMetricsToPrometheus 定时将所有用户的原子累加值同步到 Prometheus
func (c *SessionPerfCollector) flushMetricsToPrometheus() {
	c.userAggregators.Range(func(key, value any) bool {
		userName := key.(string)
		sanitizedUser := c.sanitizeUser(userName)
		agg := value.(*UserTrafficAggregator)
		m := c.getOrUpdateUserMetrics(sanitizedUser)

		// 获取当前总累加值
		currT1Bytes := agg.T1Bytes.Load()
		currT1Dur := agg.T1Dur.Load()
		currT3Bytes := agg.T3Bytes.Load()
		currT3Dur := agg.T3Dur.Load()
		currUpBytes := agg.UpBytes.Load()

		// 1. T1 增量与实时速度
		deltaT1Bytes := currT1Bytes - agg.lastT1Bytes
		deltaT1Dur := currT1Dur - agg.lastT1Dur
		if deltaT1Bytes > 0 {
			m.t1Bytes.Add(float64(deltaT1Bytes))
			agg.lastT1Bytes = currT1Bytes

			// 计算即时 Mbps (每 5 秒采样一次)
			if deltaT1Dur > 0 {
				mbps := (float64(deltaT1Bytes) / 1024 / 1024) / time.Duration(deltaT1Dur).Seconds()
				metrics.T1Throughput.WithLabelValues(userName, c.nodeID).Observe(mbps)
			}
			agg.lastT1Dur = currT1Dur
		}

		// 2. T3 增量与实时速度
		deltaT3Bytes := currT3Bytes - agg.lastT3Bytes
		deltaT3Dur := currT3Dur - agg.lastT3Dur
		if deltaT3Bytes > 0 {
			m.t3Bytes.Add(float64(deltaT3Bytes))
			m.txBytes.Add(float64(deltaT3Bytes))
			agg.lastT3Bytes = currT3Bytes

			if deltaT3Dur > 0 {
				mbps := (float64(deltaT3Bytes) / 1024 / 1024) / time.Duration(deltaT3Dur).Seconds()
				metrics.T3Throughput.WithLabelValues(userName, c.nodeID).Observe(mbps)
			}
			agg.lastT3Dur = currT3Dur
		}

		// 3. 上传增量
		deltaUpBytes := currUpBytes - agg.lastUpBytes
		if deltaUpBytes > 0 {
			m.rxBytes.Add(float64(deltaUpBytes))
			agg.lastUpBytes = currUpBytes
		}

		// 4. 同步 UserTraffic Gauge (当前节点的总累计)
		metrics.UserTraffic.WithLabelValues(sanitizedUser, c.nodeID).Set(float64(currT1Bytes + currUpBytes))

		return true
	})
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

// 已移除旧的 handleTrafficDelta，流量统计现在通过聚合器原子更新

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

	// 【过滤保护】：只对真正有数据的会话进行 ClickHouse 写入
	if (record.T1Count == 0 && record.UploadBytes == 0) || record.TargetDomain == "" {
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
	if !c.chEnabled || c.db == nil {
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
	tx, err := c.db.Begin()
	if err != nil {
		logx.Errorf("SessionPerfCollector Flush: Begin transaction error: %v", err)
		return
	}
	defer tx.Rollback()

	const insertSQL = `INSERT INTO session_perf (
		session_id, user_name, target_domain, country_code, 
		pop_node_id, client_node_id,
		duration_sec, t1_bytes_mb, t1_speed_mbps, t1_count,
		t2_avg_us, t2_total_ms, t2_count,
		t3_bytes_mb, t3_speed_mbps, t3_count, bottleneck, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	stmt, err := tx.Prepare(insertSQL)
	if err != nil {
		logx.Errorf("SessionPerfCollector Flush: Prepare error: %v", err)
		return
	}
	defer stmt.Close()

	for _, r := range toFlush {
		createdAt := time.Unix(r.Timestamp, 0)
		_, err := stmt.Exec(
			r.SessionID, r.UserName, r.TargetDomain, r.CountryCode,
			r.PopNodeID, r.ClientNodeID,
			r.DurationSec, r.T1BytesMB, r.T1SpeedMBps, r.T1Count,
			r.T2AvgUs, r.T2TotalMs, r.T2Count,
			r.T3BytesMB, r.T3SpeedMBps, r.T3Count, r.Bottleneck,
			createdAt,
		)
		if err != nil {
			logx.Errorf("SessionPerfCollector Flush: Exec error: %v", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		logx.Errorf("SessionPerfCollector Flush: Commit error: %v", err)
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
	if !c.chEnabled || c.db == nil {
		return nil, fmt.Errorf("ClickHouse not enabled")
	}

	rows, err := c.db.Query(`
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
