package model

import (
	"encoding/json"
	"testing"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

func TestIPLocation(t *testing.T) {
	conf := redis.RedisConf{Host: "127.0.0.1:6379", Type: "node"}
	rdb := redis.MustNewRedis(conf)

	scanHash(rdb, redisKeyIPLocation, func(field, value string) error {
		// t.Logf("ip:%s", field)
		loc := IPLocation{}
		err := json.Unmarshal([]byte(value), &loc)
		if err != nil {
			t.Errorf("err:%v", err)
		} else {
			t.Logf("%v", value)
		}
		return nil
	})
}

func scanHash(rdb *redis.Redis, key string, fn func(field, value string) error) error {

	var cursor uint64 = 0

	for {
		keys, cur, err := rdb.Hscan(key, cursor, "", 100)
		if err != nil {
			return err
		}

		// keys: field1, value1, field2, value2 ...
		for i := 0; i < len(keys); i += 2 {
			field := keys[i]
			value := keys[i+1]

			if err := fn(field, value); err != nil {
				return err
			}
		}

		cursor = cur
		if cursor == 0 {
			break
		}
	}

	return nil
}
