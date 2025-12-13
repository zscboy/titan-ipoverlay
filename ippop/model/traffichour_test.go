package model

import (
	"context"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestTrafficOneHour(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	users := map[string]int64{
		"test1": 1024,
		"test2": 1024 * 2,
		"test3": 1024 * 3,
		"test4": 300 * 1024 * 1024 * 1024,
	}

	err := AddUsersTrafficOneHour(context.TODO(), rd, users)
	if err != nil {
		t.Logf("AddUsersTrafficFiveMinutes failed:%v", err)
		return
	}

	traffics, err := ListUserTrafficPerHour(context.TODO(), rd, "test1", time.Now().Add(-time.Hour*(24*60)).Unix(), time.Now().Unix())
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("test1 traffics:%v", traffics)

	traffics, err = ListAllTrafficPerHour(context.TODO(), rd, 1)
	if err != nil {
		t.Logf("ListUserTrafficPer5Min failed:%v", err)
		return
	}

	t.Logf("all traffics:%v", traffics)
}

func TestBitMove(t *testing.T) {
	// data := 87845368758278 & ((1 << oneDayDataBit) - 1)
	// t.Logf("data:%#v", data)
	data := 90596966400 & ((1 << oneDayDataBit) - 1)
	t.Logf("data:%d", data)

}
