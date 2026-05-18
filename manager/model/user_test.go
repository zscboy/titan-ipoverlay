package model

import (
	"testing"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestSaveUser(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	// user := User{
	// 	UserName: "abc",
	// 	PopID:    "abc",
	// }

	userName := "abc"
	popID := "abc"
	if err := SetUserPop(rd, userName, []string{popID}); err != nil {
		t.Errorf("save user %v", err)
		return
	}
	t.Logf("save user success")
}
func TestUserMultiplePops(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rd := redis.MustNewRedis(conf)
	user := "test_multi_pop_user"

	// Clean up first
	_ = DeleteUser(rd, user)

	// Set first pop
	err := SetUserPop(rd, user, []string{"pop1"})
	if err != nil {
		t.Fatalf("set user pop1: %v", err)
	}

	// Set second pop
	err = SetUserPop(rd, user, []string{"pop2"})
	if err != nil {
		t.Fatalf("set user pop2: %v", err)
	}

	// Get all pops
	pops, err := GetUserPops(rd, user)
	if err != nil {
		t.Fatalf("get all user pops: %v", err)
	}
	if len(pops) != 2 || pops[0] != "pop1" || pops[1] != "pop2" {
		t.Errorf("expected [pop1, pop2], got %v", pops)
	}

	// Clean up
	_ = DeleteUser(rd, user)
}
