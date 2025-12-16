package yuanren

import (
	"context"
	"fmt"
	"sync"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/logic/util"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type ModifyUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewModifyUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *ModifyUserLogic {
	return &ModifyUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *ModifyUserLogic) ModifyUser(req *types.ModifyUserReq) error {
	pops := l.svcCtx.Config.Yuanren.Pops
	if len(pops) == 0 {
		return fmt.Errorf("not config pops to yuanren user")
	}
	// check pop if exist
	popServers := make([]*svc.Pop, 0, len(pops))
	for _, popID := range pops {
		server := l.svcCtx.Pops[popID]
		if server == nil {
			return fmt.Errorf("pop %s not found", popID)
		}

		popServers = append(popServers, server)
	}

	// check user if exist
	popID, err := model.GetUserPop(l.svcCtx.Redis, req.UserName)
	if err != nil {
		return fmt.Errorf("get user %s failed: %v", req.UserName, err)
	}

	if len(popID) == 0 {
		return fmt.Errorf("user %s not exist", req.UserName)
	}

	mu := sync.Mutex{}
	results := make([]*types.UserOperationResp, 0, len(popServers))
	var lastErr error
	wg := sync.WaitGroup{}
	wg.Add(len(pops))
	for _, popServer := range popServers {
		pop := popServer
		go func() {
			defer wg.Done()

			resp, err := l.modifyUser(pop, req)
			if err != nil {
				logx.Errorf("createUserWithPop error:%v", err)
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			mu.Lock()
			if resp.Success {
				results = append(results, resp)
			} else {
				lastErr = fmt.Errorf("%s", resp.ErrMsg)
			}
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(results) != len(popID) {
		return lastErr
	}
	return nil
}

func (l *ModifyUserLogic) modifyUser(pop *svc.Pop, req *types.ModifyUserReq) (resp *types.UserOperationResp, err error) {
	in := &serverapi.ModifyUserReq{UserName: req.UserName}
	if req.TrafficLimit != nil {
		in.TrafficLimit = util.ToTrafficLimitReq(req.TrafficLimit)
	}

	if req.Route != nil {
		in.Route = util.ToRouteReq(req.Route)
	}

	modifyUserResp, err := pop.API.ModifyUser(l.ctx, in)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: modifyUserResp.Success, ErrMsg: modifyUserResp.ErrMsg}, nil
}
