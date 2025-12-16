package model

import (
	"context"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestTraffic5min(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	nowTimestamp := time.Now().Unix()
	trafficRecores := []*TrafficRecore{
		&TrafficRecore{
			"test1",
			nowTimestamp,
			1024,
		},
		&TrafficRecore{
			"test1",
			nowTimestamp,
			1024 * 2,
		},
		&TrafficRecore{
			"test1",
			nowTimestamp,
			1024 * 3,
		},
		&TrafficRecore{
			"test1",
			nowTimestamp,
			300 * 1024 * 1024 * 1024,
		},
	}

	err := AddUsersTraffic5Minutes(context.TODO(), rd, trafficRecores)
	if err != nil {
		t.Logf("AddUsersTrafficFiveMinutes failed:%v", err)
		return
	}

	traffics, err := ListUserTrafficPer5Min(context.TODO(), rd, "test1", time.Now().Add(-time.Hour*(24*60)).Unix(), time.Now().Unix())
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("test1 traffics:%v", traffics)

}
