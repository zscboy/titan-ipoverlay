package model

import (
	"context"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestTrafficOneDay(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)

	nowTimestamp := time.Now().Unix()
	trafficRecores := []*TrafficRecore{
		{
			"test1",
			nowTimestamp,
			1024,
		},
		{
			"test1",
			nowTimestamp,
			1024 * 2,
		},
		{
			"test1",
			nowTimestamp,
			1024 * 3,
		},
		{
			"test1",
			nowTimestamp,
			300 * 1024 * 1024 * 1024,
		},
	}

	err := AddUsersTrafficOneDay(context.TODO(), rd, trafficRecores)
	if err != nil {
		t.Logf("AddUsersTrafficFiveMinutes failed:%v", err)
		return
	}

	traffics, err := ListUserTrafficPerDay(context.TODO(), rd, "test4", time.Now().Add(-time.Hour*(24*60)).Unix(), time.Now().Unix())
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("test1 traffics:%v", traffics)

	traffics, err = ListAllTrafficPerDay(context.TODO(), rd, 24*7)
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("all traffics:%v", traffics)
}

// 获取全部用户最近 N 小时的流量
func TestListTrafficPerDay(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	// users := map[string]int64{
	// 	"test1": 1024,
	// 	"test2": 1024 * 2,
	// 	"test3": 1024 * 3,
	// 	"test4": 24 * 60 * 60 * 1024 * 1024 * 1024,
	// }

	// err := AddUsersTrafficOneDay(context.TODO(), rd, users)
	// if err != nil {
	// 	t.Logf("AddUsersTrafficFiveMinutes failed:%v", err)
	// 	return
	// }

	traffics, err := ListUserTrafficPerDay(context.TODO(), rd, "test4", time.Now().Add(-time.Hour*(24*60)).Unix(), time.Now().Unix())
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("test1 traffics:%v", traffics)

	// traffics, err = ListAllTrafficPerDay(context.TODO(), rd, 24*7)
	// if err != nil {
	// 	t.Logf("ListUserTrafficPer5Min failed:%v", err)
	// 	return
	// }

	t.Logf("all traffics:%v", traffics)
}
