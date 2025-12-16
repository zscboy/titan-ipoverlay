package push

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type Relation struct {
	redis *redis.Redis
}

func (r *Relation) HandleStats(data any) error {
	// data.(types.Relation)
	return nil
}
