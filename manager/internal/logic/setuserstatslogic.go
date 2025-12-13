// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package logic

import (
	"context"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type SetUserStatsLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewSetUserStatsLogic(ctx context.Context, svcCtx *svc.ServiceContext) *SetUserStatsLogic {
	return &SetUserStatsLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *SetUserStatsLogic) SetUserStats(req *types.SetUserStatsReq) (resp *types.SetUserStatsResp, err error) {
	if len(req.Stats) == 0 {
		return
	}

	trafficMap := make(map[string]int64)
	for _, info := range req.Stats {
		trafficMap[info.Username] = info.Traffic
	}

	if err := model.AddUsersTrafficOneDay(l.ctx, l.svcCtx.Redis, trafficMap); err != nil {
		logx.Errorf("SetUserStats AddUsersTrafficOneDay failed:%v", err)
	}

	if err := model.AddUsersTrafficOneHour(l.ctx, l.svcCtx.Redis, trafficMap); err != nil {
		logx.Errorf("SetUserStats AddUsersTrafficOneHour failed:%v", err)
	}

	if err := model.AddUsersTraffic5Minutes(l.ctx, l.svcCtx.Redis, trafficMap); err != nil {
		logx.Errorf("SetUserStats AddUsersTraffic5Minutes failed:%v", err)
	}

	// if err := model.AddUsersTotalTraffic(l.ctx, l.svcCtx.Redis, trafficMap); err != nil {
	// 	logx.Errorf("SetUserStats AddUsersTotalTraffic failed:%v", err)
	// }

	return
}
