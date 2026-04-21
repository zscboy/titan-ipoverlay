package logic

import (
	"context"
	"sort"

	"titan-ipoverlay/manager/internal/svc"
	"titan-ipoverlay/manager/internal/types"
	"titan-ipoverlay/manager/model"

	"github.com/zeromicro/go-zero/core/logx"
)

type GetIPPackStatusLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
}

func NewGetIPPackStatusLogic(ctx context.Context, svcCtx *svc.ServiceContext) *GetIPPackStatusLogic {
	return &GetIPPackStatusLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
	}
}

func (l *GetIPPackStatusLogic) GetIPPackStatus(req *types.GetIPPackStatusReq) (*types.GetIPPackStatusResp, error) {
	statuses, err := model.GetIPPackStatuses(l.ctx, l.svcCtx.Redis, req.IP)
	if err != nil {
		return nil, err
	}

	items := make([]*types.IPPackStatusItem, 0, len(statuses))
	for pack, status := range statuses {
		if req.Pack != "" && req.Pack != pack {
			continue
		}
		items = append(items, &types.IPPackStatusItem{
			Pack:         pack,
			Label:        status.Label,
			SuccessCount: status.SuccessCount,
			FailureCount: status.FailureCount,
			LastUpdated:  status.LastUpdated,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Pack < items[j].Pack
	})

	return &types.GetIPPackStatusResp{
		IP:       req.IP,
		Statuses: items,
	}, nil
}
