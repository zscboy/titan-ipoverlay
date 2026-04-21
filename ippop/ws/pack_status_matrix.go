package ws

import (
	"context"
	"sync"

	"titan-ipoverlay/ippop/businesspack"
	"titan-ipoverlay/ippop/model"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type PackDecision int

const (
	PackDecisionDeny PackDecision = iota
	PackDecisionGray
	PackDecisionUnknown
	PackDecisionAllow
)

type PackStatusMatrix struct {
	redis *redis.Redis
	cache sync.Map
}

func NewPackStatusMatrix(rds *redis.Redis) *PackStatusMatrix {
	return &PackStatusMatrix{redis: rds}
}

func (m *PackStatusMatrix) GetDecision(ip, pack string) PackDecision {
	if ip == "" || pack == "" {
		return PackDecisionUnknown
	}
	key := ip + "|" + pack
	if value, ok := m.cache.Load(key); ok {
		return value.(PackDecision)
	}

	status, err := model.GetIPPackStatus(context.Background(), m.redis, ip, pack)
	if err != nil {
		logx.Errorf("PackStatusMatrix.GetDecision ip=%s pack=%s err=%v", ip, pack, err)
		return PackDecisionUnknown
	}

	decision := packLabelToDecision("")
	if status != nil {
		decision = packLabelToDecision(status.Label)
	}
	m.cache.Store(key, decision)
	return decision
}

func (m *PackStatusMatrix) ReportResult(ip, pack string, success bool) {
	if ip == "" || pack == "" {
		return
	}

	status, err := model.ReportIPPackResult(context.Background(), m.redis, ip, pack, success)
	if err != nil {
		logx.Errorf("PackStatusMatrix.ReportResult ip=%s pack=%s success=%v err=%v", ip, pack, success, err)
		return
	}

	m.cache.Store(ip+"|"+pack, packLabelToDecision(status.Label))
}

func packLabelToDecision(label string) PackDecision {
	switch label {
	case businesspack.LabelAllow:
		return PackDecisionAllow
	case businesspack.LabelGray:
		return PackDecisionGray
	case businesspack.LabelDeny:
		return PackDecisionDeny
	default:
		return PackDecisionUnknown
	}
}
