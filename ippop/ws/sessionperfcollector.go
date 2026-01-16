package ws

import (
	"context"
	"fmt"
	"sync"
	"time"

	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/metrics"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// 批量刷新配置
	maxBufferSize = 100              // 缓冲区满时触发写入
	flushInterval = 10 * time.Second // 定时刷新间隔
)

// ClickHouse 建表 SQL (按小时分区，3天TTL)
const createTableSQL = `
CREATE TABLE IF NOT EXISTS session_perf (
    session_id    String,
    user_name     String,
    target_domain String,
    country_code  String,
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
PARTITION BY toYYYYMMDDhh(created_at)
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
	Bottleneck   string  `json:"btn"`
	Timestamp    int64   `json:"ts"`
}

// collectorEvent 内部事件
type collectorEvent struct {
	isStart  bool
	userName string
	record   SessionPerfRecord
}

// SessionPerfCollector 会话性能数据批量收集器
type SessionPerfCollector struct {
	clickhouse  driver.Conn
	chEnabled   bool
	buffer      []SessionPerfRecord
	bufferLock  sync.Mutex
	metricsChan chan collectorEvent
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSessionPerfCollector 创建新的收集器
func NewSessionPerfCollector(chConfig config.ClickHouse) *SessionPerfCollector {
	ctx, cancel := context.WithCancel(context.Background())

	c := &SessionPerfCollector{
		chEnabled:   chConfig.Enable,
		buffer:      make([]SessionPerfRecord, 0, maxBufferSize),
		metricsChan: make(chan collectorEvent, 2000),
		ctx:         ctx,
		cancel:      cancel,
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
			// 自动建表
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

	logx.Info("SessionPerfCollector started")

	for {
		select {
		case event := <-c.metricsChan:
			if event.isStart {
				c.handleSessionStart(event.userName)
			} else {
				c.handleSessionEnd(event.record)
			}
		case <-ticker.C:
			c.Flush()
		case <-c.ctx.Done():
			c.Flush() // 退出前最后刷新一次
			if c.clickhouse != nil {
				c.clickhouse.Close()
			}
			logx.Info("SessionPerfCollector stopped")
			return
		}
	}
}

// handleSessionStart 处理会话开始指标
func (c *SessionPerfCollector) handleSessionStart(userName string) {
	metrics.SOCKS5Connections.Inc()
	metrics.TotalSessions.Inc()
	metrics.ActiveSessions.Inc()
}

// handleSessionEnd 处理会话结束指标
func (c *SessionPerfCollector) handleSessionEnd(r SessionPerfRecord) {
	// 基础指标
	metrics.ActiveSessions.Dec()
	metrics.SessionDuration.Observe(r.DurationSec)

	// T1 指标
	if r.T1Count > 0 {
		metrics.T1Throughput.WithLabelValues(r.UserName).Observe(r.T1SpeedMBps)
		metrics.T1Bytes.WithLabelValues(r.UserName).Add(r.T1BytesMB * 1024 * 1024)
	}

	// T2 指标
	if r.T2Count > 0 {
		metrics.T2ProcessingTime.WithLabelValues(r.UserName).Observe(float64(r.T2AvgUs))
	}

	// T3 指标
	if r.T3Count > 0 {
		metrics.T3Throughput.WithLabelValues(r.UserName).Observe(r.T3SpeedMBps)
		metrics.T3Bytes.WithLabelValues(r.UserName).Add(r.T3BytesMB * 1024 * 1024)
	}

	// 瓶颈检测
	metrics.BottleneckDetection.WithLabelValues(r.Bottleneck, r.UserName).Inc()

	// 多维度流量统计 (Task 4)
	totalBytes := (r.T1BytesMB + r.T3BytesMB) * 1024 * 1024
	if r.TargetDomain != "" {
		metrics.DomainTraffic.WithLabelValues(r.UserName, r.TargetDomain).Add(totalBytes)
	}
	if r.CountryCode != "" {
		metrics.CountryTraffic.WithLabelValues(r.UserName, r.CountryCode).Add(totalBytes)
	}
}

// Stop 停止收集器
func (c *SessionPerfCollector) Stop() {
	c.cancel()
}

// ReportSessionStart 异步上报会话开始
func (c *SessionPerfCollector) ReportSessionStart(userName string) {
	select {
	case c.metricsChan <- collectorEvent{isStart: true, userName: userName}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping start event")
	}
}

// Collect 添加数据到缓冲区
func (c *SessionPerfCollector) Collect(record SessionPerfRecord) {
	// 1. 异步发送到处理通道（用于 Prometheus 指标）
	select {
	case c.metricsChan <- collectorEvent{isStart: false, record: record}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping end record")
	}

	// 2. 批量写入 ClickHouse
	if !c.chEnabled {
		return
	}

	c.bufferLock.Lock()
	c.buffer = append(c.buffer, record)
	needFlush := len(c.buffer) >= maxBufferSize
	c.bufferLock.Unlock()

	if needFlush {
		go c.Flush() // 异步刷新，避免阻塞调用者
	}
}

// Flush 批量写入 ClickHouse
func (c *SessionPerfCollector) Flush() {
	if !c.chEnabled || c.clickhouse == nil {
		return
	}

	c.bufferLock.Lock()
	if len(c.buffer) == 0 {
		c.bufferLock.Unlock()
		return
	}
	// 交换缓冲区
	toFlush := c.buffer
	c.buffer = make([]SessionPerfRecord, 0, maxBufferSize)
	c.bufferLock.Unlock()

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
	c.bufferLock.Lock()
	defer c.bufferLock.Unlock()
	return len(c.buffer)
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
