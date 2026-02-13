package config

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
	"github.com/zeromicro/go-zero/zrpc"
)

type JwtAuth struct {
	AccessSecret string
	AccessExpire int64
}

type Pop struct {
	Name        string
	Id          string
	Area        string
	CountryCode string
	RpcClient   zrpc.RpcClientConf
	WSURL       string
	MaxCount    int
}

type GeoAPI struct {
	API string
	Key string
}

type StrategyRule struct {
	Key    string
	PopIds []string
}

type StrategyConfig struct {
	BlacklistPopId string
	RegionRules    []StrategyRule
	VendorRules    []StrategyRule
	DefaultPopId   string
}

type Config struct {
	rest.RestConf
	Redis   redis.RedisConf
	JwtAuth JwtAuth
	// todo: will move to center server
	Pops      []Pop
	Strategy  StrategyConfig
	GeoAPI    GeoAPI
	Whitelist []string `json:",optional"`
}
