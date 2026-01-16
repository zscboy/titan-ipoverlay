package model

const (
	redisKeyUser     = "titan:user:%s"
	redisKeyUserZset = "titan:user:zset"
	// redisKeyUserTrafficDay     = "titan:traffic:%s"
	// redisKeyUserTrafficDayAll  = "titan:trafficall"

	redisKeyUserTraffic5min    = "titan:traffic5min:%s"
	redisKeyUserTraffic5minAll = "titan:traffic5minall"

	redisKeyUserTrafficHour    = "titan:traffichour:%s"
	redisKeyUserTrafficHourAll = "titan:traffichourmall"

	redisKeyUserTrafficDay    = "titan:trafficday:%s"
	redisKeyUserTrafficDayAll = "titan:trafficdayall"

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
