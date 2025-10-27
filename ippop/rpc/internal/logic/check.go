package logic

import (
	"fmt"
	"time"
	"titan-ipoverlay/ippop/api/model"
	"titan-ipoverlay/ippop/rpc/pb"

	"github.com/zeromicro/go-zero/core/stores/redis"
)

const (
	routeModeType = iota
	routeModeTypeAuto
	routeModeTypeManual
	routeModeTypeTimed
)

func checkRoute(redis *redis.Redis, route *pb.Route) error {
	if route == nil {
		return fmt.Errorf("route is empty")
	}
	if isInvalidRouteMode(route.Mode) {
		return fmt.Errorf("invalid route mode %d", route.Mode)
	}

	// check node if exist or used by other user
	if len(route.NodeId) != 0 {
		node, err := model.GetNode(redis, route.NodeId)
		if err != nil {
			return err
		}

		if node == nil {
			return fmt.Errorf("node %s not exist", route.NodeId)
		}

		if len(node.BindUser) != 0 {
			return fmt.Errorf("node %s alreay used by user %s", route.NodeId, node.BindUser)
		}
	}

	return nil
}

func isInvalidRouteMode(mode int32) bool {
	if mode != routeModeTypeManual && mode != routeModeTypeAuto && mode != routeModeTypeTimed {
		return true
	}

	return false
}

func checkTraffic(trafficLimit *pb.TrafficLimit) error {
	if trafficLimit.EndTime <= trafficLimit.StartTime {
		return fmt.Errorf("invalid traffic start time %d and end time %d", trafficLimit.StartTime, trafficLimit.EndTime)
	}

	if trafficLimit.EndTime < time.Now().Unix() {
		return fmt.Errorf("traffic end time is out of date", trafficLimit.EndTime)
	}

	if trafficLimit.TotalTraffic <= 0 {
		return fmt.Errorf("invalid total traffic ", trafficLimit.TotalTraffic)
	}

	return nil
}
