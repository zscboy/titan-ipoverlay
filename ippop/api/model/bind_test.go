package model

import (
	"context"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestBind(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	id := "95e66ae2-5bca-11f0-9654-00163e0ced7c"

	user := &User{
		UserName:                   "abc",
		PasswordMD5:                "abc",
		StartTime:                  time.Now().Unix(),
		EndTime:                    time.Now().Unix(),
		TotalTraffic:               100000000,
		RouteMode:                  1,
		RouteNodeID:                id,
		UpdateRouteIntervalMinutes: 0,
		LastRouteSwitchTime:        0,
	}
	if err := BindNodeWithNewUser(context.TODO(), rd, id, user); err != nil {
		t.Log(err.Error())
	}
}

func TestUnbind(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	UnbindNode(context.TODO(), rd, "95e66ae2-5bca-11f0-9654-00163e0ced7c")
}

func TestSwitchNode(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	user, err := GetUser(rd, "test")
	if err != nil {
		t.Log(err.Error())
		return
	}
	err = SwitchNodeByUser(context.Background(), rd, user, "9b8a4d28-7368-11f0-bfe0-00163e023040")
	if err != nil {
		t.Log(err.Error())
		return
	}
}
