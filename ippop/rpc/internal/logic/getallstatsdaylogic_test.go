package logic

import (
	"context"
	"testing"

	"titan-ipoverlay/ippop/rpc/internal/svc"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestGetAllTrafficPerDay(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)

	l := GetAllStatsDayLogic{ctx: context.Background(), svcCtx: &svc.ServiceContext{Redis: rd}}
	resp, err := l.GetAllStatsDay(&pb.AllStatsDayReq{Days: 30})
	if err != nil {
		t.Logf("GetAllStats5Min %v", err)
		return
	}
	for _, trend := range resp.TrendDatas {
		t.Logf("all trend:%d, %d", trend.Bandwidth, trend.Traffic)
	}
	// t.Logf("all traffics:%#v", resp.TrendDatas)
}
