package model

const (
	redisKeyUser               = "titan:user:%s"
	redisKeyUserZset           = "titan:user:zset"
	redisKeyUserRouteScheduler = "titan:user:routescheduler"
	redisKeyUserTraffic        = "titan:traffic:%s"

	redisKeyNode     = "titan:node:%s"
	redisKeyNodeZset = "titan:node:zset"
	// key expire
	redisKeyNodeOnline = "titan:node:online"
	// sort set,
	redisKeyNodeBind = "titan:node:bind"
	// sort set, free = unbind + online
	redisKeyNodeFree      = "titan:node:free"
	redisKeyNodeBlacklist = "titan:node:blacklist"
)
