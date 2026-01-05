package model

import (
	"encoding/json"
	"fmt"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

type IPLocation struct {
	Country  string `json:"country"`
	Province string `json:"province"`
	City     string `json:"city"`
	IP       string `json:"ip"`
}

func SaveIPLocation(rdb *redis.Redis, loc *IPLocation) error {
	if loc == nil || loc.IP == "" {
		return fmt.Errorf("invalid ipLocation")
	}

	data, err := json.Marshal(loc)
	if err != nil {
		return err
	}

	return rdb.Hset(redisKeyIPLocation, loc.IP, string(data))
}

func GetIPLocation(rdb *redis.Redis, ip string) (*IPLocation, error) {
	jsonStr, err := rdb.Hget(redisKeyIPLocation, ip)
	if err != nil {
		return nil, err
	}

	if len(jsonStr) == 0 {
		return nil, fmt.Errorf("ip %s location not exist", ip)
	}

	loc := IPLocation{}
	if err := json.Unmarshal([]byte(jsonStr), &loc); err != nil {
		return nil, err
	}

	return &loc, nil
}
