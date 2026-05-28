package logic

import (
	"context"
	"strings"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

type ListGlobalUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewListGlobalUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ListGlobalUserLogic {
	return &ListGlobalUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ListGlobalUserLogic) ListGlobalUser(req *types.ListGlobalUserReq) (resp *types.ListGlobalUserResp, err error) {
	// Fetch all users from manager's Redis users hash
	usersMap, err := l.svcCtx.Redis.Hgetall("titan:manager:users")
	if err != nil {
		return nil, err
	}

	var users []*types.GlobalUser
	for userName, popIDsStr := range usersMap {
		var popIDs []string
		if len(popIDsStr) > 0 {
			popIDs = strings.Split(popIDsStr, ",")
		}
		u := &types.GlobalUser{
			UserName: userName,
			PopIds:   popIDs,
		}
		users = append(users, u)
	}

	// Handle pagination
	total := len(users)
	start := req.Start
	end := req.End
	if start < 0 {
		start = 0
	}
	if end <= 0 || end > total {
		end = total
	}
	if start < total {
		if end < start {
			end = start
		}
		users = users[start:end]
	} else {
		users = []*types.GlobalUser{}
	}

	return &types.ListGlobalUserResp{Users: users, Total: total}, nil
}
