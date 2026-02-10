package ws

import (
	"fmt"
	"sync/atomic"
	"time"

	"titan-ipoverlay/ippop/config"
)

// SessionPerfStats 会话性能统计
// T1: Client → IPPop (WebSocket 接收) - 从远程客户端接收数据
// T2: 内部处理 - WebSocket 读取完成到 SOCKS5 写入开始之间的处理时间
// T3: IPPop → 用户 (SOCKS5 发送) - 发送数据到本地 SOCKS5 用户
type SessionPerfStats struct {
	SessionID    string
	UserName     string
	TargetDomain string // 目标域名
	CountryCode  string // 国家码 (新增)
	StartTime    time.Time
	EndTime      atomic.Pointer[time.Time]

	// 配置
	perfConfig *config.PerfMonitoring

	// 收集器引用
	collector  *SessionPerfCollector
	aggregator *UserTrafficAggregator // 缓存用户级别的聚合器，实现热路径无锁直连

	// T1: Client → IPPop (WebSocket 接收)
	T1BytesReceived atomic.Int64
	T1Duration      atomic.Int64
	T1Count         atomic.Int64

	// T2: 内部处理
	T2Duration atomic.Int64
	T2Count    atomic.Int64

	// T3: IPPop → 用户 (SOCKS5 发送)
	T3BytesSent atomic.Int64
	T3Duration  atomic.Int64
	T3Count     atomic.Int64

	// T4: 用户 → IPPop (SOCKS5 读取)
	T4BytesReceived atomic.Int64
}

// NewSessionPerfStats 创建新的会话性能统计
func NewSessionPerfStats(sessionID, userName, targetDomain, countryCode string, perfConfig *config.PerfMonitoring, collector *SessionPerfCollector) *SessionPerfStats {
	s := &SessionPerfStats{
		SessionID:    sessionID,
		UserName:     userName,
		TargetDomain: targetDomain,
		CountryCode:  countryCode,
		StartTime:    time.Now(),
		perfConfig:   perfConfig,
		collector:    collector,
	}

	if collector != nil {
		s.aggregator = collector.GetAggregator(userName)
	}

	return s
}

// AddT1Read 添加 T1 读取统计（Client → POP）
func (s *SessionPerfStats) AddT1Read(bytes int64, duration time.Duration) {
	s.T1BytesReceived.Add(bytes)
	s.T1Duration.Add(int64(duration))
	s.T1Count.Add(1)

	if s.aggregator != nil {
		s.aggregator.T1Bytes.Add(bytes)
		s.aggregator.T1Dur.Add(int64(duration))
	}
}

// AddT2Process 添加 T2 处理统计
func (s *SessionPerfStats) AddT2Process(duration time.Duration) {
	s.T2Duration.Add(int64(duration))
	s.T2Count.Add(1)

	if s.aggregator != nil {
		s.aggregator.T2Dur.Add(int64(duration))
	}
}

// AddT3Write 添加 T3 写入统计（POP → Target）
func (s *SessionPerfStats) AddT3Write(bytes int64, duration time.Duration) {
	s.T3BytesSent.Add(bytes)
	s.T3Duration.Add(int64(duration))
	s.T3Count.Add(1)

	if s.aggregator != nil {
		s.aggregator.T3Bytes.Add(bytes)
		s.aggregator.T3Dur.Add(int64(duration))
	}
}

// AddT4Read 添加 T4 读取统计（User → POP，上传方向）
func (s *SessionPerfStats) AddT4Read(bytes int64) {
	s.T4BytesReceived.Add(bytes)

	if s.aggregator != nil {
		s.aggregator.UpBytes.Add(bytes)
	}
}

// Close 关闭会话统计并输出日志
func (s *SessionPerfStats) Close() {
	now := time.Now()
	s.EndTime.Store(&now)

	totalDuration := now.Sub(s.StartTime)

	t1BytesReceived := s.T1BytesReceived.Load()
	t1Duration := s.T1Duration.Load()
	t1Count := s.T1Count.Load()
	t2Duration := s.T2Duration.Load()
	t2Count := s.T2Count.Load()
	t3BytesSent := s.T3BytesSent.Load()
	t3Duration := s.T3Duration.Load()
	t3Count := s.T3Count.Load()
	t4BytesReceived := s.T4BytesReceived.Load()

	// 计算速率 (确保 duration 至少为 1us，避免极短会话导致的除零或 0 速率)
	t1Speed := float64(0)
	t3Speed := float64(0)

	t1DurSec := time.Duration(t1Duration).Seconds()
	if t1DurSec <= 0 && t1BytesReceived > 0 {
		t1DurSec = 0.000001 // 1us min
	}
	if t1DurSec > 0 {
		t1Speed = float64(t1BytesReceived) / t1DurSec / 1024 / 1024 // MB/s
	}

	t3DurSec := time.Duration(t3Duration).Seconds()
	if t3DurSec <= 0 && t3BytesSent > 0 {
		t3DurSec = 0.000001 // 1us min
	}
	if t3DurSec > 0 {
		t3Speed = float64(t3BytesSent) / t3DurSec / 1024 / 1024 // MB/s
	}

	// 计算平均处理时间
	t2AvgUs := int64(0)
	if t2Count > 0 {
		t2Dur := time.Duration(t2Duration)
		if t2Dur <= 0 {
			t2Dur = time.Microsecond
		}
		t2AvgUs = t2Dur.Microseconds() / t2Count
	}

	// 检测瓶颈
	bottleneck := s.DetectBottleneck(t1Speed, t3Speed, t2AvgUs)

	// 提交到收集器异步处理
	if s.collector != nil {
		// 【过滤保护】：只对真正有数据的会话进行 ClickHouse 写入和详细统计收集
		if (t1Count == 0 && t4BytesReceived == 0) || s.TargetDomain == "" {
			return
		}

		s.collector.Collect(&SessionPerfRecord{
			SessionID:    s.SessionID,
			UserName:     s.UserName,
			TargetDomain: s.TargetDomain,
			CountryCode:  s.CountryCode,
			DurationSec:  totalDuration.Seconds(),
			T1BytesMB:    float64(t1BytesReceived) / 1024 / 1024,
			T1SpeedMBps:  t1Speed,
			T1Count:      t1Count,
			T2AvgUs:      t2AvgUs,
			T2TotalMs:    time.Duration(t2Duration).Milliseconds(),
			T2Count:      t2Count,
			T3BytesMB:    float64(t3BytesSent) / 1024 / 1024,
			T3SpeedMBps:  t3Speed,
			T3Count:      t3Count,
			UploadBytes:  t4BytesReceived,
			Bottleneck:   bottleneck,
			Timestamp:    time.Now().Unix(),
		})
	}
}

// DetectBottleneck 检测性能瓶颈
func (s *SessionPerfStats) DetectBottleneck(t1Speed, t3Speed float64, t2AvgUs int64) string {
	// 如果 T1 速度明显慢于 T3（客户端网络慢）
	if t1Speed > 0 && t3Speed > 0 && t1Speed < t3Speed*0.9 {
		return "t1_slow_client_network"
	}

	// 如果 T3 速度明显慢于 T1（目标网络慢）
	if t1Speed > 0 && t3Speed > 0 && t3Speed < t1Speed*0.9 {
		return "t3_slow_target_network"
	}

	// 如果内部处理时间过长（>10ms）
	if t2AvgUs > 10000 {
		return "t2_slow_processing"
	}

	// 如果数据量太小，无法判断
	if s.T1BytesReceived.Load() < 1024*100 && s.T3BytesSent.Load() < 1024*100 {
		return "too_small_to_detect"
	}

	return "balanced"
}

// GetSummary 获取统计摘要（用于 Prometheus）
func (s *SessionPerfStats) GetSummary() map[string]interface{} {
	totalDuration := time.Since(s.StartTime)
	if endTime := s.EndTime.Load(); endTime != nil {
		totalDuration = endTime.Sub(s.StartTime)
	}

	t1BytesReceived := s.T1BytesReceived.Load()
	t1Duration := s.T1Duration.Load()
	t1Count := s.T1Count.Load()
	t2Duration := s.T2Duration.Load()
	t2Count := s.T2Count.Load()
	t3BytesSent := s.T3BytesSent.Load()
	t3Duration := s.T3Duration.Load()
	t3Count := s.T3Count.Load()

	t1Speed := float64(0)
	t3Speed := float64(0)
	t2AvgUs := int64(0)

	if t1Duration > 0 {
		t1Speed = float64(t1BytesReceived) / time.Duration(t1Duration).Seconds()
	}

	if t3Duration > 0 {
		t3Speed = float64(t3BytesSent) / time.Duration(t3Duration).Seconds()
	}

	if t2Count > 0 {
		t2AvgUs = time.Duration(t2Duration).Microseconds() / t2Count
	}

	return map[string]interface{}{
		"session_id":       s.SessionID,
		"user_name":        s.UserName,
		"duration_seconds": totalDuration.Seconds(),
		"t1_bytes":         t1BytesReceived,
		"t1_speed_bps":     t1Speed,
		"t1_count":         t1Count,
		"t2_avg_us":        t2AvgUs,
		"t2_total_ms":      time.Duration(t2Duration).Milliseconds(),
		"t2_count":         t2Count,
		"t3_bytes":         t3BytesSent,
		"t3_speed_bps":     t3Speed,
		"t3_count":         t3Count,
		"bottleneck":       s.DetectBottleneck(t1Speed, t3Speed, t2AvgUs),
	}
}

// String 返回格式化的字符串
func (s *SessionPerfStats) String() string {
	summary := s.GetSummary()
	return fmt.Sprintf(
		"Session[%s] Duration:%.1fs T1:%.2fMB@%.2fMB/s T2:avg=%dμs T3:%.2fMB@%.2fMB/s Bottleneck:%s",
		s.SessionID,
		summary["duration_seconds"],
		float64(s.T1BytesReceived.Load())/1024/1024,
		summary["t1_speed_bps"].(float64)/1024/1024,
		summary["t2_avg_us"],
		float64(s.T3BytesSent.Load())/1024/1024,
		summary["t3_speed_bps"].(float64)/1024/1024,
		summary["bottleneck"],
	)
}
