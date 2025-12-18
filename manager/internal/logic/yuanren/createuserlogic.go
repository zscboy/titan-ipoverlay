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

type CreateUserLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewCreateUserLogic(ctx context.Context, svcCtx *svc.ServiceContext) *CreateUserLogic {
	return &CreateUserLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

// TODO: 如果并发，会有问题
func (l *CreateUserLogic) CreateUser(req *types.CreateUserReq) error {
	logx.Debugf("CreateUser")
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

	if len(popID) != 0 {
		return fmt.Errorf("user %s already exist", req.UserName)
	}

	mu := sync.Mutex{}
	results := make([]*types.CreateUserResp, 0, len(popServers))
	var lastErr error
	wg := sync.WaitGroup{}
	wg.Add(len(pops))
	for _, popServer := range popServers {
		pop := popServer
		go func() {
			defer wg.Done()

			resp, err := l.createUserWithPop(pop, req)
			if err != nil {
				logx.Errorf("createUserWithPop error:%v", err)
				mu.Lock()
				lastErr = err
				mu.Unlock()
				return
			}

			mu.Lock()
			results = append(results, resp)
			mu.Unlock()
		}()
	}

	wg.Wait()

	if len(results) != len(popServers) {
		// TODO　rollback, delete all user
		for _, pop := range popServers {
			resp, err := pop.API.DeleteUser(context.Background(), &serverapi.DeleteUserReq{UserName: req.UserName})
			if err != nil {
				logx.Errorf("rollback delete user failed:%v", err)
				continue
			}

			if !resp.Success {
				logx.Errorf("rollback delete user failed:%v", resp.ErrMsg)
			}
		}
		logx.Debugf("lastErr:%v", lastErr)
		return lastErr
	}
	logx.Debugf("CreateUser %s success", req.UserName)

	// TODO:这里有问题，目前只保存用户的第一个Pop,表明用户已经创建了，不能根据用户索引pop
	return model.SetUserPop(l.svcCtx.Redis, req.UserName, pops[0])

}

func (l *CreateUserLogic) createUserWithPop(pop *svc.Pop, req *types.CreateUserReq) (resp *types.CreateUserResp, err error) {
	in := &serverapi.CreateUserReq{UserName: req.UserName, Password: req.Password, UploadRateLimite: req.UploadRateLimit, DownloadRateLimit: req.DownloadRateLimit}
	if req.TrafficLimit != nil {
		in.TrafficLimit = util.ToTrafficLimitReq(req.TrafficLimit)
	}

	if req.Route != nil {
		in.Route = util.ToRouteReq(req.Route)
	}

	createUserResp, err := pop.API.CreateUser(l.ctx, in)
	if err != nil {
		return nil, err
	}

	return util.ToCreateUserResp(createUserResp), nil
}
