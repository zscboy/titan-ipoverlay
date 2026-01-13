package ws

import (
	"fmt"
	"math/rand"
	"time"

	"titan-ipoverlay/ippop/config"
	"titan-ipoverlay/ippop/metrics"

	"github.com/zeromicro/go-zero/core/logx"
)

// SessionPerfStats 会话性能统计
// T1: Source (Client → POP) - SOCKS5 客户端到 IPPop
// T2: Process (内部处理) - IPPop 内部处理时间
// T3: Target (POP → 目标) - IPPop 到目标服务器
type SessionPerfStats struct {
	SessionID string
	UserName  string
	StartTime time.Time
	EndTime   time.Time

	// 配置
	perfConfig *config.PerfMonitoring

	// T1: Client → POP (SOCKS5 读取)
	T1BytesReceived int64
	T1Duration      time.Duration
	T1Count         int64

	// T2: 内部处理
	T2Duration time.Duration
	T2Count    int64

	// T3: POP → Target (写入目标)
	T3BytesSent int64
	T3Duration  time.Duration
	T3Count     int64
}

// NewSessionPerfStats 创建新的会话性能统计
func NewSessionPerfStats(sessionID, userName string, perfConfig *config.PerfMonitoring) *SessionPerfStats {
	return &SessionPerfStats{
		SessionID:  sessionID,
		UserName:   userName,
		StartTime:  time.Now(),
		perfConfig: perfConfig,
	}
}

// AddT1Read 添加 T1 读取统计（Client → POP）
func (s *SessionPerfStats) AddT1Read(bytes int64, duration time.Duration) {
	s.T1BytesReceived += bytes
	s.T1Duration += duration
	s.T1Count++
}

// AddT2Process 添加 T2 处理统计
func (s *SessionPerfStats) AddT2Process(duration time.Duration) {
	s.T2Duration += duration
	s.T2Count++
}

// AddT3Write 添加 T3 写入统计（POP → Target）
func (s *SessionPerfStats) AddT3Write(bytes int64, duration time.Duration) {
	s.T3BytesSent += bytes
	s.T3Duration += duration
	s.T3Count++
}

// Close 关闭会话统计并输出日志
func (s *SessionPerfStats) Close() {
	s.EndTime = time.Now()

	totalDuration := s.EndTime.Sub(s.StartTime)

	// 计算速率
	t1Speed := float64(0)
	t3Speed := float64(0)

	if s.T1Duration.Seconds() > 0 {
		t1Speed = float64(s.T1BytesReceived) / s.T1Duration.Seconds() / 1024 / 1024 // MB/s
	}

	if s.T3Duration.Seconds() > 0 {
		t3Speed = float64(s.T3BytesSent) / s.T3Duration.Seconds() / 1024 / 1024 // MB/s
	}

	// 计算平均处理时间
	t2AvgUs := int64(0)
	if s.T2Count > 0 {
		t2AvgUs = s.T2Duration.Microseconds() / s.T2Count
	}

	// 检测瓶颈
	bottleneck := s.DetectBottleneck(t1Speed, t3Speed, t2AvgUs)

	// 上报 Prometheus 指标
	if s.T1BytesReceived > 0 || s.T3BytesSent > 0 {
		s.reportToPrometheus(t1Speed, t3Speed, t2AvgUs, bottleneck)
	}

	// 日志输出策略（优化性能）
	shouldLog := false

	// 使用配置值，如果配置为 nil 则使用默认值
	logSampleRate := 0.01
	enableVerboseLog := false
	abnormalDurationThreshold := 60 * time.Second

	if s.perfConfig != nil {
		logSampleRate = s.perfConfig.LogSampleRate
		enableVerboseLog = s.perfConfig.EnableVerboseLog
		abnormalDurationThreshold = time.Duration(s.perfConfig.AbnormalDurationSeconds) * time.Second
	}

	if enableVerboseLog {
		// 详细模式：采样记录
		shouldLog = rand.Float64() < logSampleRate
	} else {
		// 生产模式：只记录异常会话
		shouldLog = (bottleneck != "balanced" && bottleneck != "too_small_to_detect") ||
			totalDuration > abnormalDurationThreshold
	}

	if shouldLog {
		logx.Infof("TCPProxy close: Session[%s] User[%s] Duration:%.1fs | "+
			"T1: %.2fMB @ %.2fMB/s (reads:%d) | "+
			"T2: avg=%dμs total=%dms (ops:%d) | "+
			"T3: %.2fMB @ %.2fMB/s (writes:%d) | "+
			"Bottleneck: %s",
			s.SessionID,
			s.UserName,
			totalDuration.Seconds(),
			float64(s.T1BytesReceived)/1024/1024,
			t1Speed,
			s.T1Count,
			t2AvgUs,
			s.T2Duration.Milliseconds(),
			s.T2Count,
			float64(s.T3BytesSent)/1024/1024,
			t3Speed,
			s.T3Count,
			bottleneck,
		)
	}
}

// reportToPrometheus 上报指标到 Prometheus
func (s *SessionPerfStats) reportToPrometheus(t1Speed, t3Speed float64, t2AvgUs int64, bottleneck string) {
	// T1 指标
	if t1Speed > 0 {
		metrics.T1Throughput.WithLabelValues(s.UserName).Observe(t1Speed)
		metrics.T1Bytes.WithLabelValues(s.UserName).Add(float64(s.T1BytesReceived))
	}

	// T2 指标
	if t2AvgUs > 0 {
		metrics.T2ProcessingTime.WithLabelValues(s.UserName).Observe(float64(t2AvgUs))
	}

	// T3 指标
	if t3Speed > 0 {
		metrics.T3Throughput.WithLabelValues(s.UserName).Observe(t3Speed)
		metrics.T3Bytes.WithLabelValues(s.UserName).Add(float64(s.T3BytesSent))
	}

	// 瓶颈检测
	metrics.BottleneckDetection.WithLabelValues(bottleneck, s.UserName).Inc()
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
	if s.T1BytesReceived < 1024*100 && s.T3BytesSent < 1024*100 {
		return "too_small_to_detect"
	}

	return "balanced"
}

// GetSummary 获取统计摘要（用于 Prometheus）
func (s *SessionPerfStats) GetSummary() map[string]interface{} {
	totalDuration := time.Since(s.StartTime)
	if !s.EndTime.IsZero() {
		totalDuration = s.EndTime.Sub(s.StartTime)
	}

	t1Speed := float64(0)
	t3Speed := float64(0)
	t2AvgUs := int64(0)

	if s.T1Duration.Seconds() > 0 {
		t1Speed = float64(s.T1BytesReceived) / s.T1Duration.Seconds()
	}

	if s.T3Duration.Seconds() > 0 {
		t3Speed = float64(s.T3BytesSent) / s.T3Duration.Seconds()
	}

	if s.T2Count > 0 {
		t2AvgUs = s.T2Duration.Microseconds() / s.T2Count
	}

	return map[string]interface{}{
		"session_id":       s.SessionID,
		"user_name":        s.UserName,
		"duration_seconds": totalDuration.Seconds(),
		"t1_bytes":         s.T1BytesReceived,
		"t1_speed_bps":     t1Speed,
		"t1_count":         s.T1Count,
		"t2_avg_us":        t2AvgUs,
		"t2_total_ms":      s.T2Duration.Milliseconds(),
		"t2_count":         s.T2Count,
		"t3_bytes":         s.T3BytesSent,
		"t3_speed_bps":     t3Speed,
		"t3_count":         s.T3Count,
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
		float64(s.T1BytesReceived)/1024/1024,
		summary["t1_speed_bps"].(float64)/1024/1024,
		summary["t2_avg_us"],
		float64(s.T3BytesSent)/1024/1024,
		summary["t3_speed_bps"].(float64)/1024/1024,
		summary["bottleneck"],
	)
}
