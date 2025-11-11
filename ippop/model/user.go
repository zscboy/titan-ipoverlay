package model

import (
	"context"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type RouteMode int

const (
	RouteModeAuto RouteMode = iota + 1
	RouteModeManual
	RouteModeTimed
)

func (m RouteMode) String() string {
	switch m {
	case RouteModeAuto:
		return "auto"
	case RouteModeManual:
		return "manual"
	case RouteModeTimed:
		return "timed"
	default:
		return fmt.Sprintf("unknown(%d)", int(m))
	}
}

type User struct {
	UserName    string `redis:"user_name"`
	PasswordMD5 string `redis:"password_md5"`
	// start time timestamp
	StartTime int64 `redis:"start_time"`
	// end time timestamp
	EndTime int64 `redis:"end_time"`
	// Total traffic allowed to the user
	// unit Bytes
	TotalTraffic int64 `redis:"total_traffic"`
	// unit Bytes
	CurrentTraffic int64 `redis:"current_traffic"`

	// 1.manual, 2.auto, 3.Timed
	RouteMode int `redis:"route_mode"`
	// if NodeID is empty, will auto allocate a node for user
	RouteNodeID string `redis:"route_node_id"`
	// RouteMode=Timed
	// If >0, switch every N minutes
	UpdateRouteIntervalMinutes int `redis:"update_route_interval_minutes"`
	// RouteMode=Timed
	// If IntervalMinutes=0, trigger at specific UTC minutes of day
	// example: utc+8 10:30=(10-8)*60+30
	UpdateRouteUtcMinuteOfDay int `redis:"update_route_utc_minute_of_day"`
	// Unix timestamp of last route switch
	LastRouteSwitchTime int64 `redis:"last_route_switch_time"`
	Off                 bool  `redis:"off"`
	UploadRateLimit     int64 `redis:"upload_rate_limit"`
	DownloadRateLimit   int64 `redis:"download_rate_limit"`
}

func SaveUser(redis *redis.Redis, user *User) error {
	m, err := structToMap(user)
	if err != nil {
		return err
	}

	key := fmt.Sprintf(redisKeyUser, user.UserName)
	return redis.Hmset(key, m)
}

// if user not exist, will return nil, nil
func GetUser(redis *redis.Redis, userName string) (*User, error) {
	key := fmt.Sprintf(redisKeyUser, userName)
	m, err := redis.Hgetall(key)
	if err != nil {
		return nil, err
	}

	if len(m) == 0 {
		return nil, nil
	}

	user := &User{}
	err = mapToStruct(m, user)
	if err != nil {
		return nil, err
	}

	return user, nil
}

// TODO: use pip
func DeleteUser(redis *redis.Redis, userName string) error {
	key := fmt.Sprintf(redisKeyUser, userName)
	_, err := redis.Del(key)
	if err != nil {
		return err
	}

	_, err = redis.Zrem(redisKeyUserZset, userName)
	return err
}

func ZaddUser(redis *redis.Redis, userName string) error {
	_, err := redis.Zadd(redisKeyUserZset, time.Now().Unix(), userName)
	return err
}

func ListUser(ctx context.Context, redis *redis.Redis, start, end int) ([]*User, error) {
	userNames, err := redis.Zrevrange(redisKeyUserZset, int64(start), int64(end))
	if err != nil {
		return nil, err
	}

	return ListUserWithNames(ctx, redis, userNames)
}

func ListUserWithNames(ctx context.Context, redis *redis.Redis, userNames []string) ([]*User, error) {
	pipe, err := redis.TxPipeline()
	if err != nil {
		return nil, err
	}

	for _, userName := range userNames {
		key := fmt.Sprintf(redisKeyUser, userName)
		pipe.HGetAll(ctx, key)
	}

	cmds, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	users := make([]*User, 0, len(cmds))
	for _, cmd := range cmds {
		result, err := cmd.(*goredis.MapStringStringCmd).Result()
		if err != nil {
			logx.Errorf("ListNode parse result failed:%s", err.Error())
			continue
		}

		user := User{}
		err = mapToStruct(result, &user)
		if err != nil {
			logx.Errorf("ListNode mapToStruct error:%s", err.Error())
			continue
		}

		users = append(users, &user)
	}

	return users, nil
}

func GetUserLen(redis *redis.Redis) (int, error) {
	return redis.Zcard(redisKeyUserZset)
}

func AddUserTotalTraffic(redis *redis.Redis, userName string, traffic int) error {
	key := fmt.Sprintf(redisKeyUser, userName)
	_, err := redis.Hincrby(key, "current_traffic", traffic)
	return err
}

func AddUsersTotalTraffic(ctx context.Context, redis *redis.Redis, users map[string]int64) error {
	pipe, err := redis.TxPipeline()
	if err != nil {
		return err
	}

	for userName, traffic := range users {
		if traffic <= 0 {
			continue
		}

		key := fmt.Sprintf(redisKeyUser, userName)
		pipe.HIncrBy(ctx, key, "current_traffic", traffic)

	}

	_, err = pipe.Exec(ctx)
	return err
}
