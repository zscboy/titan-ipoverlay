package model

import (
	"context"
	"testing"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestTraffic5min(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	users := map[string]int64{
		"test1": 1024,
		"test2": 1024 * 2,
		"test3": 1024 * 3,
		"test4": 300 * 1024 * 1024 * 1024,
	}

	err := AddUsersTrafficFiveMinutes(context.TODO(), rd, users)
	if err != nil {
		t.Logf("AddUsersTrafficFiveMinutes failed:%v", err)
		return
	}

	traffics, err := ListUserTrafficPer5Min(context.TODO(), rd, "test1", 1)
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("test1 traffics:%v", traffics)

	traffics, err = ListAllTrafficPer5Min(context.TODO(), rd, 1)
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("all traffics:%v", traffics)
}
