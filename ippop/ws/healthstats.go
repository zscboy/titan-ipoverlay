package ws

import "time"

const (
	maxFailureRate = 0.3
	resetAfter     = 60 * time.Minute
	minSamples     = 5
)

type HealthStats struct {
	successCount int64
	failureCount int64
	invalidSince time.Time
}

// 小于5次认为失败率无效
// 判断是否有效节点，失败率要小于0.4
// 如果失败率大于0.4小于1， 则20分钟后重置
func (h *HealthStats) checkValid() bool {
	total := h.successCount + h.failureCount
	if total < minSamples {
		return true
	}

	failureRate := float64(h.failureCount) / float64(total)

	// 失败率正常，节点有效，顺便清空无效状态
	if failureRate <= maxFailureRate {
		h.invalidSince = time.Time{}
		return true
	}

	// 失败率为 1，说明全失败，直接认为不可用（是否重置可按需要调整）
	if failureRate >= 1 {
		return false
	}

	// failureRate 在 (0.4, 1) 之间
	now := time.Now()

	// 第一次进入失败状态，记录时间
	if h.invalidSince.IsZero() {
		h.invalidSince = now
		return false
	}

	// 超过 60 分钟，重置统计，给节点一次“重生”机会
	if now.Sub(h.invalidSince) >= resetAfter {
		h.successCount = 0
		h.failureCount = 0
		h.invalidSince = time.Time{}
		return true
	}

	return false
}

func (h *HealthStats) isTotalFailed() bool {
	total := h.successCount + h.failureCount
	if h.failureCount == total {
		return true
	}
	return false
}

func (h *HealthStats) isHalfFailed() bool {
	if h.invalidSince.IsZero() {
		return false
	}
	return true
}
