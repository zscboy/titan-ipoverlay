package config

import (
	api "titan-ipoverlay/ippop/api/export"
	rpc "titan-ipoverlay/ippop/rpc/export"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

type Config struct {
	APIServer api.APIServerConfig
	RPCServer rpc.RPCServerConfig
	Redis     redis.RedisConf
	Log       logx.LogConf
	HTTPProxy string `json:",optional"`
	Pprof     Pprof  `json:",optional"`
	// TLSKeyPair TLSKeyPair
}

type Pprof struct {
	Enable     bool
	ListenAddr string
}

type TLSKeyPair struct {
	Cert string
	Key  string
}
