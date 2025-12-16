package model

const (
	redisKeyUsers    = "titan:manager:users"
	redisKeyNodes    = "titan:manager:nodes"
	redisKeyPopNodes = "titan:manager:pop:%s"

	redisKeyUserTraffic5min    = "titan:manager:traffic5min:%s"
	redisKeyUserTraffic5minAll = "titan:manager:traffic5minParent:%s"

	redisKeyUserTrafficHour    = "titan:manager:traffichour:%s"
	redisKeyUserTrafficHourAll = "titan:manager:traffichourParent:%s"

	redisKeyUserTrafficDay    = "titan:manager:trafficday:%s"
	redisKeyUserTrafficDayAll = "titan:manager:trafficdayParent:%s"

	redisKeyUserParentMap = "titan:manager:user:parent"
)
