package yuanren

import (
	"context"
	"fmt"
	"sync"

	"titan-ipoverlay/ippop/rpc/serverapi"
	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type StartOrStopUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewStartOrStopUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *StartOrStopUserLogic {
	return &StartOrStopUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *StartOrStopUserLogic) StartOrStopUser(req *types.StartOrStopUserReq) error {
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

			resp, err := l.startOrStopUser(pop, req)
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

func (l *StartOrStopUserLogic) startOrStopUser(pop *svc.Pop, req *types.StartOrStopUserReq) (resp *types.UserOperationResp, err error) {
	in := &serverapi.StartOrStopUserReq{UserName: req.UserName, Action: req.Action}
	startOrStopResp, err := pop.API.StartOrStopUser(l.ctx, in)
	if err != nil {
		return &types.UserOperationResp{ErrMsg: err.Error()}, nil
	}

	return &types.UserOperationResp{Success: startOrStopResp.Success, ErrMsg: startOrStopResp.ErrMsg}, nil
}
