package push

import (
	"context"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	buf = 10000
)

type Stats struct {
	Type string
	Data any
}

type PushManager struct {
	ch       chan Stats
	ctx      context.Context
	cancel   context.CancelFunc
	handlers map[string]StatsHandler
	redis    *redis.Redis
}

func NewPushManager(redis *redis.Redis) *PushManager {
	ctx, cancel := context.WithCancel(context.Background())

	pm := &PushManager{
		ch:       make(chan Stats, buf),
		ctx:      ctx,
		cancel:   cancel,
		redis:    redis,
		handlers: make(map[string]StatsHandler),
	}

	registerHandlers(pm)

	go pm.worker()

	return pm
}

func (p *PushManager) worker() {
	for {
		select {
		case <-p.ctx.Done():
			return
		case stat := <-p.ch:
			p.handleStat(stat)
		}
	}
}

func (p *PushManager) Close() {
	p.cancel()
	close(p.ch)
}

func (pm *PushManager) register(t string, h StatsHandler) {
	pm.handlers[t] = h
}

func (p *PushManager) Push(stats Stats) {
	select {
	case p.ch <- stats:
	default:
		logx.Errorf("stats channel full, drop: %s", stats.Type)
	}
}

func (p *PushManager) handleStat(stats Stats) {
	handler, ok := p.handlers[stats.Type]
	if ok {
		handler.HandleStats(stats.Data)
	} else {
		logx.Errorf("unknown stat type: %s", stats.Type)
	}
}
