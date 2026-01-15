package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"titan-ipoverlay/ippop/metrics"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	// 批量刷新配置
	maxBufferSize        = 100              // 缓冲区满时触发写入
	flushInterval        = 10 * time.Second // 定时刷新间隔
	sessionPerfRetention = 72 * time.Hour   // 数据保留时间 (3天)

	// Redis key 格式
	redisKeySessionPerf = "session:perf:%s" // session:perf:{YYYYMMDD}
)

// SessionPerfRecord 会话性能记录（存储到 Redis 的结构）
type SessionPerfRecord struct {
	SessionID    string  `json:"sid"`
	UserName     string  `json:"user"`
	TargetDomain string  `json:"domain"`
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
	redis       *redis.Redis
	buffer      []SessionPerfRecord
	bufferLock  sync.Mutex
	metricsChan chan collectorEvent // 改为接收通用事件
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSessionPerfCollector 创建新的收集器
func NewSessionPerfCollector(rdb *redis.Redis) *SessionPerfCollector {
	ctx, cancel := context.WithCancel(context.Background())
	return &SessionPerfCollector{
		redis:       rdb,
		buffer:      make([]SessionPerfRecord, 0, maxBufferSize),
		metricsChan: make(chan collectorEvent, 2000), // 增加缓冲区
		ctx:         ctx,
		cancel:      cancel,
	}
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
	// 1. 异步发送到处理通道
	select {
	case c.metricsChan <- collectorEvent{isStart: false, record: record}:
	default:
		logx.WithContext(c.ctx).Error("SessionPerfCollector: metrics channel full, dropping end record")
	}

	// 2. 批量写入 Redis 逻辑
	c.bufferLock.Lock()
	c.buffer = append(c.buffer, record)
	needFlush := len(c.buffer) >= maxBufferSize
	c.bufferLock.Unlock()

	if needFlush {
		go c.Flush() // 异步刷新，避免阻塞调用者
	}
}

// Flush 批量写入 Redis
func (c *SessionPerfCollector) Flush() {
	c.bufferLock.Lock()
	if len(c.buffer) == 0 {
		c.bufferLock.Unlock()
		return
	}
	// 交换缓冲区
	toFlush := c.buffer
	c.buffer = make([]SessionPerfRecord, 0, maxBufferSize)
	c.bufferLock.Unlock()

	// 使用 Pipeline 批量写入
	pipe, err := c.redis.TxPipeline()
	if err != nil {
		logx.Errorf("SessionPerfCollector Flush: TxPipeline error: %v", err)
		return
	}

	// 按天分区的 key
	key := fmt.Sprintf(redisKeySessionPerf, time.Now().Format("20060102"))
	ctx := context.Background()

	for _, r := range toFlush {
		jsonData, err := json.Marshal(r)
		if err != nil {
			logx.Errorf("SessionPerfCollector Flush: Marshal error: %v", err)
			continue
		}

		// 使用 session_id + timestamp 作为唯一标识，避免重复
		member := fmt.Sprintf("%s:%d", r.SessionID, r.Timestamp)

		pipe.ZAdd(ctx, key, goredis.Z{
			Score:  float64(r.Timestamp),
			Member: string(jsonData),
		})

		// 同时存储索引，方便按用户查询
		userKey := fmt.Sprintf("session:perf:user:%s:%s", r.UserName, time.Now().Format("20060102"))
		pipe.ZAdd(ctx, userKey, goredis.Z{
			Score:  float64(r.Timestamp),
			Member: member,
		})
		pipe.Expire(ctx, userKey, sessionPerfRetention)
	}

	// 设置主 key 过期时间
	pipe.Expire(ctx, key, sessionPerfRetention)

	_, err = pipe.Exec(ctx)
	if err != nil {
		logx.Errorf("SessionPerfCollector Flush: Exec error: %v", err)
	} else {
		logx.Debugf("SessionPerfCollector Flush: wrote %d records", len(toFlush))
	}
}

// BufferSize 返回当前缓冲区大小（用于监控）
func (c *SessionPerfCollector) BufferSize() int {
	c.bufferLock.Lock()
	defer c.bufferLock.Unlock()
	return len(c.buffer)
}
