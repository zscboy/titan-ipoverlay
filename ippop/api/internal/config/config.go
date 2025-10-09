package config

import (
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/rest"
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

type Config struct {
	rest.RestConf
	//lint:ignore SA5008 go-zero allows "optional","inherit" in struct tags
	Redis   redis.RedisConf `json:",optional,inherit"`
	JwtAuth JwtAuth
	Socks5  Socks5
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Domain      string `json:",optional"`
	FilterRules FilterRules
}
