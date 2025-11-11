package model

import (
	"context"
	"testing"
	"time"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestSaveUser(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	user := User{
		UserName:       "abc",
		PasswordMD5:    "aaaaa",
		StartTime:      time.Now().Unix(),
		EndTime:        time.Now().Unix(),
		TotalTraffic:   1000 * 1024 * 1024 * 1024,
		CurrentTraffic: 0,
		RouteMode:      1,
		RouteNodeID:    "95e66ae2-5bca-11f0-9654-00163e0ced7c",
	}

	if err := SaveUser(rd, &user); err != nil {
		t.Errorf("save user %v", err)
		return
	}
	t.Logf("save user success")
}

func TestGetUser(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	user, err := GetUser(rd, "abc")
	if err != nil {
		t.Errorf("get user %v", err)
		return
	}

	t.Logf("user:%v", user)
}

func TestListUser(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	users, err := ListUser(context.Background(), rd, 0, 100)
	if err != nil {
		t.Errorf("get user %v", err)
		return
	}

	for _, uer := range users {
		t.Logf("user:%v", uer)
	}
	// t.Logf("user:%v", users)
}
