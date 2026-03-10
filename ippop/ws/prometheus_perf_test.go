package ws

import (
	"sync"
	"testing"
	"titan-ipoverlay/ippop/metrics"
)

// BenchmarkPrometheusMetrics 测试 Prometheus 指标的性能
func BenchmarkPrometheusMetrics(b *testing.B) {
	userName := "test_user"

	b.Run("Counter.Add", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			metrics.T1Bytes.WithLabelValues(userName).Add(1024)
		}
	})

	b.Run("Histogram.Observe", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			metrics.T1Throughput.WithLabelValues(userName).Observe(10.5)
		}
	})

	b.Run("Full_T1_T2_T3_Report", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			// 模拟完整的 reportToPrometheus 调用
			metrics.T1Throughput.WithLabelValues(userName).Observe(10.5)
			metrics.T1Bytes.WithLabelValues(userName).Add(1048576)
			metrics.T2ProcessingTime.WithLabelValues(userName).Observe(100)
			metrics.T3Throughput.WithLabelValues(userName).Observe(12.3)
			metrics.T3Bytes.WithLabelValues(userName).Add(2097152)
			metrics.BottleneckDetection.WithLabelValues("balanced", userName).Inc()
		}
	})
}

// BenchmarkPrometheusMetricsConcurrent 测试并发性能
func BenchmarkPrometheusMetricsConcurrent(b *testing.B) {
	userName := "test_user"

	b.Run("Concurrent_Counter", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				metrics.T1Bytes.WithLabelValues(userName).Add(1024)
			}
		})
	})

	b.Run("Concurrent_Histogram", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				metrics.T1Throughput.WithLabelValues(userName).Observe(10.5)
			}
		})
	})

	b.Run("Concurrent_Full_Report", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				metrics.T1Throughput.WithLabelValues(userName).Observe(10.5)
				metrics.T1Bytes.WithLabelValues(userName).Add(1048576)
				metrics.T2ProcessingTime.WithLabelValues(userName).Observe(100)
				metrics.T3Throughput.WithLabelValues(userName).Observe(12.3)
				metrics.T3Bytes.WithLabelValues(userName).Add(2097152)
				metrics.BottleneckDetection.WithLabelValues("balanced", userName).Inc()
			}
		})
	})
}

// BenchmarkPrometheusWithMultipleUsers 测试多用户场景
func BenchmarkPrometheusWithMultipleUsers(b *testing.B) {
	users := []string{"user1", "user2", "user3", "user4", "user5"}

	b.Run("Multiple_Users_Sequential", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			userName := users[i%len(users)]
			metrics.T1Bytes.WithLabelValues(userName).Add(1024)
		}
	})

	b.Run("Multiple_Users_Concurrent", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				userName := users[i%len(users)]
				metrics.T1Bytes.WithLabelValues(userName).Add(1024)
				i++
			}
		})
	})
}

// BenchmarkLockContention 对比锁竞争
func BenchmarkLockContention(b *testing.B) {
	var mu sync.Mutex
	var rwmu sync.RWMutex
	var counter uint64

	b.Run("Mutex_Lock", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		})
	})

	b.Run("RWMutex_RLock", func(b *testing.B) {
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				rwmu.RLock()
				_ = counter
				rwmu.RUnlock()
			}
		})
	})

	b.Run("Prometheus_WithLabelValues", func(b *testing.B) {
		userName := "test_user"
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				metrics.T1Bytes.WithLabelValues(userName).Add(1)
			}
		})
	})
}
