package ws

import (
	"context"
	"time"
	"titan-ipoverlay/ippop/model"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	userRouteSchedulerInterval = 30 // Check every 30 seconds
)

type UserRouteScheduler struct {
	userChan chan *model.User // Channel for user route switch tasks
	stopChan chan struct{}    // Signal for graceful shutdown
	tm       *TunnelManager
}

func newUserRouteScheduler(tm *TunnelManager) *UserRouteScheduler {
	return &UserRouteScheduler{
		userChan: make(chan *model.User, 100), // Buffered channel to avoid blocking producer
		stopChan: make(chan struct{}),
		tm:       tm,
	}
}

// Start the scheduler
func (scheduler *UserRouteScheduler) start() {
	// Start the route switch worker
	go scheduler.routeSwitchWorker()

	logx.Info("UserRouteScheduler.start")

	// Periodically scan users
	ticker := time.NewTicker(userRouteSchedulerInterval * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			scheduler.checkUsers()
		case <-scheduler.stopChan:
			logx.Info("user route scheduler stopped")
			close(scheduler.userChan)
			return
		}
	}
}

// Stop the scheduler
func (scheduler *UserRouteScheduler) stop() {
	close(scheduler.stopChan)
}

// Scan all users and send those that need a route switch to the channel
func (scheduler *UserRouteScheduler) checkUsers() {
	users, err := scheduler.listUser()
	if err != nil {
		logx.Errorf("list user failed: %v", err)
		return
	}

	for _, user := range users {
		if scheduler.isUserNeedToSwithRoute(user) {
			select {
			case scheduler.userChan <- user:
				logx.Infof("queued user %v for route switch", user.UserName)
			default:
				logx.Errorf("user channel full, drop user %v", user.UserName)
			}
		}
	}
}

// Worker goroutine that actually performs user route switches
func (scheduler *UserRouteScheduler) routeSwitchWorker() {
	for user := range scheduler.userChan {
		if err := scheduler.switchUserRoute(user); err != nil {
			logx.Errorf("user %s switch route failed:%v", user.UserName, err)
		}
	}
}

func (scheduler *UserRouteScheduler) listUser() ([]*model.User, error) {
	start := 0
	count := 20
	users := make([]*model.User, 0)
	for {
		userNames, err := model.ListUserFromSchedulerList(scheduler.tm.redis, start, start+count-1)
		if err != nil {
			return nil, err
		}

		if len(userNames) <= 0 {
			break
		}

		us, err := model.ListUserWithNames(context.Background(), scheduler.tm.redis, userNames)
		if err != nil {
			return nil, err
		}

		users = append(users, us...)

		if len(userNames) < count {
			break
		}

		start = start + count
	}

	return users, nil
}

func (scheduler *UserRouteScheduler) isUserNeedToSwithRoute(user *model.User) bool {
	if user.RouteMode != int(model.RouteModeTimed) {
		return false
	}

	lastUpdate := time.Unix(user.LastRouteSwitchTime, 0)

	// Check based on interval mode
	if user.UpdateRouteIntervalMinutes > 0 {
		nextSwitch := lastUpdate.Add(time.Duration(user.UpdateRouteIntervalMinutes) * time.Minute)
		return nextSwitch.Before(time.Now())
	}

	// Check based on daily time
	now := time.Now().UTC()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switchTime := dayStart.Add(time.Duration(user.UpdateRouteUtcMinuteOfDay) * time.Minute)

	if lastUpdate.Before(switchTime) && now.After(switchTime) {
		return true
	}

	return false
}

func (scheduler *UserRouteScheduler) switchUserRoute(user *model.User) error {
	return scheduler.tm.switchNodeForUser(user)
}
