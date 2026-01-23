package config

import (
	rpc "titan-ipoverlay/ippop/rpc/export"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
)

type TunnelSelectPolicy string

const (
	TunnelSelectRandom TunnelSelectPolicy = "random"
	TunnelSelectRound  TunnelSelectPolicy = "round"
)

type NodeAllocateStrategy string

const (
	NodeAllocateRedis  NodeAllocateStrategy = "redis"
	NodeAllocateIPPool NodeAllocateStrategy = "ippool"
)

type JwtAuth struct {
	AccessSecret string
	AccessExpire int64
}

type Socks5 struct {
	Addr         string
	ServerIP     string
	UDPPortStart int
	UDPPortEnd   int
	EnableAuth   bool
	TCPTimeout   int64
	UDPTimeout   int64
}

type WS struct {
	rest.RestConf
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	Domain string `json:",optional"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	DownloadRateLimit int64 `json:",default=655360"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	UploadRateLimit int64 `json:",default=655360"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	TunnelSelectPolicy TunnelSelectPolicy `json:",default=random"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	NodeAllocateStrategy NodeAllocateStrategy `json:",default=redis"`
}

type FilterRule struct {
	//lint:ignore SA5008 go-zero allows "options" in struct tags
	Type  string `json:"type,options=domain|ip|port"`
	Value string `json:"value"`
	//lint:ignore SA5008 go-zero allows "options" in struct tags
	Action string `json:"action,options=allow|deny"`
}

type FilterRules struct {
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Rules []FilterRule `json:",optional"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	DefaultAction string `json:",default=allow"`
}

type Pprof struct {
	Enable     bool
	ListenAddr string
}

type Config struct {
	// APIServer api.APIServerConfig
	WS        WS
	RPCServer rpc.RPCServerConfig
	Redis     redis.RedisConf
	Log       logx.LogConf
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	HTTPProxy string `json:",optional"`
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Pprof Pprof `json:",optional"`

	JwtAuth JwtAuth
	Socks5  Socks5
	// Domain      string `json:",optional"`
	FilterRules FilterRules
	// TLSKeyPair TLSKeyPair
}
