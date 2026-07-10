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

type Metrics struct {
	Enable     bool
	ListenAddr string
}

type PerfMonitoring struct {
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	LogSampleRate float64 `json:",default=0.01"` // 日志采样率（0.01 = 1%，0.1 = 10%，1.0 = 100%）
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	EnableVerboseLog bool `json:",default=false"` // 是否启用详细日志（生产环境建议 false）
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	AbnormalDurationSeconds int64 `json:",default=60"` // 异常会话阈值（秒）
}

type QoSConf struct {
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	RedlineSpeedKbps int64 `json:",default=300"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	CircuitBreakerKbps int64 `json:",default=50"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PatienceWindowSec int64 `json:",default=10"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	RollingWindowSec int64 `json:",default=15"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	StrikeLimit int64 `json:",default=3"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	EwmaAlpha float64 `json:",default=0.2"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	EnableBandwidthBlacklist bool `json:",default=false"`
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	ProbationDurationSec int64 `json:",default=3600"`
}

type ClickHouse struct {
	Enable   bool
	Addr     string // e.g., "127.0.0.1:9000"
	Database string
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Username string `json:",optional"`
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Password string `json:",optional"`
}

type TrafficStats struct {
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	EnableUserTraffic bool `json:",default=false"`
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
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	Metrics Metrics `json:",optional"`

	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	PerfMonitoring PerfMonitoring `json:",optional"`

	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	ClickHouse ClickHouse `json:",optional"`

	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	QoS QoSConf `json:",optional"`

	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	TrafficStats TrafficStats `json:",optional"`

	// P2C 负载感知就近 polling(默认关):每请求随机抽 Depth 个空闲盒,按
	// score = 实测RTT + LoadPenaltyMs×在途会话数 取低者;每第 RRInterval 个请求
	// 走轮询队首保底(任何盒子最多隔 池大小×RRInterval 个请求必被选中)。
	// Depth: 0/1=关闭(行为与原混播逐字节一致);建议灰度 2,上限 4。
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PollingP2CDepth int `json:",default=0"`
	// RRInterval: 保底间隔 R,范围[1,10],1=逐请求保底(语义等价原混播,安全滑轨);
	// >10 会被钳制:R 过大时新盒/冷启动盒只能靠保底喂养,饥饿上界失去意义。
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PollingP2CRRInterval int `json:",default=5"`
	// LoadPenaltyMs: λ,每个在途会话折算的毫秒惩罚。<50 时 P2C 被强制关闭——
	// 纯 RTT 竞速会把并发堆到快盒上(仿真:λ=0 时 1007 断连率翻倍)。
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PollingP2CLoadPenaltyMs int `json:",default=100"`
	// MinPool: 空闲 IP 少于此数时整体退化为原混播(薄池护栏)。
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PollingP2CMinPool int `json:",default=16"`
	// MaxBoxSessions: 单盒在途会话硬上限,0=关;开启时建议锚定住宅盒实际容量(个位数~十位数)。
	//lint:ignore SA5008 go-zero allows "default" in struct tags
	PollingP2CMaxBoxSessions int `json:",default=0"`

	JwtAuth JwtAuth
	Socks5  Socks5
	// Domain      string `json:",optional"`
	FilterRules FilterRules
	//lint:ignore SA5008 go-zero allows "optional" in struct tags
	NodeID string `json:",optional"`
	// TLSKeyPair TLSKeyPair
}

func (c Config) GetNodeID() string {
	if c.NodeID != "" {
		return c.NodeID
	}
	return "default-node"
}
